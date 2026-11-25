// Package user 用户管理。
package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/i18n"
	"github.com/keel-framework/keel/pkg/query"
)

// Service 用户业务逻辑。
type Service struct {
	db    *gorm.DB
	cfg   *config.Config
	log   *zap.Logger
	authz *authz.Loader
	mem   MembershipFiller
}

// MembershipFiller 由会员模块实现：补齐用户列表的等级信息。
// 关闭会员模块时为 nil，用户管理照常可用，只是不展示等级。
//
// 接口声明在消费方而不是会员包里，用户模块才不需要 import 会员模块——
// 这样会员模块可以整体裁掉，用户模块不受影响。
type MembershipFiller interface {
	Fill(ctx context.Context, users []*model.User) error
}

// NewService 创建用户服务。mem 可为空：关闭会员模块时不展示等级信息。
func NewService(db *gorm.DB, cfg *config.Config, log *zap.Logger, al *authz.Loader, mem MembershipFiller) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cfg: cfg, log: log, authz: al, mem: mem}
}

// SetMembershipFiller 在会员模块装配完成后再注入。装配顺序由依赖图决定，
// 用户模块可能先于会员模块构造，因此不走构造函数参数。
func (s *Service) SetMembershipFiller(mem MembershipFiller) { s.mem = mem }

// Scope 数据权限范围。
type Scope struct {
	All      bool
	DeptIDs  []string
	SelfOnly string
}

// ResolveScope 依据当前用户角色计算数据范围。
func (s *Service) ResolveScope(ctx context.Context, identity *authn.Identity) Scope {
	if identity == nil {
		return Scope{SelfOnly: ""}
	}
	if identity.Super {
		return Scope{All: true}
	}
	var scopes []int8
	err := s.db.WithContext(ctx).
		Table("sys_role AS r").
		Select("r.data_scope").
		Joins("JOIN sys_user_role AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ? AND r.status = 1 AND r.deleted_at IS NULL", identity.UserID).
		Pluck("r.data_scope", &scopes).Error
	if err != nil || len(scopes) == 0 {
		return Scope{SelfOnly: identity.UserID}
	}
	best := scopes[0]
	for _, v := range scopes {
		if v < best {
			best = v
		}
	}
	switch best {
	case model.DataScopeAll:
		return Scope{All: true}
	case model.DataScopeDept:
		return Scope{DeptIDs: []string{identity.DeptID}}
	case model.DataScopeDeptChild:
		return Scope{DeptIDs: s.deptSubtree(ctx, identity.DeptID)}
	default:
		return Scope{SelfOnly: identity.UserID}
	}
}

func (s *Service) deptSubtree(ctx context.Context, root string) []string {
	if root == "" {
		return nil
	}
	var all []model.Dept
	if err := s.db.WithContext(ctx).Select("id", "parent_id").Find(&all).Error; err != nil {
		return []string{root}
	}
	children := map[string][]string{}
	for _, d := range all {
		children[d.ParentID] = append(children[d.ParentID], d.ID)
	}
	out := []string{root}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// List 分页查询用户。deptFilter 非空时与数据权限取交集，避免越权查询。
func (s *Service) List(ctx context.Context, p *query.Params, sc Scope, deptFilter []string) ([]*model.User, int64, error) {
	allowed := map[string]string{
		"username": "username", "nickname": "nickname", "phone": "phone",
		"email": "email", "status": "status", "dept_id": "dept_id",
		"membership_level_id": "membership_level_id",
		"created_at":          "created_at",
	}
	base := s.db.WithContext(ctx).Model(&model.User{})
	if len(deptFilter) > 0 {
		base = base.Where("dept_id IN ?", deptFilter)
	}
	if !sc.All {
		switch {
		case sc.SelfOnly != "":
			base = base.Where("id = ?", sc.SelfOnly)
		case len(sc.DeptIDs) > 0:
			base = base.Where("dept_id IN ?", sc.DeptIDs)
		default:
			base = base.Where("1 = 0")
		}
	}
	db := p.Filter(base, allowed)
	db = applyMembershipFilter(db, p.Filters["membershipState"])
	db = query.Search(db, p.Keyword, "username", "nickname", "phone", "email")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.User
	err := db.Order(p.Order(map[string]string{
		"created_at": "created_at", "username": "username", "last_login_at": "last_login_at", "id": "id",
	}, "created_at DESC")).Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	if err := s.attachRelations(ctx, list); err != nil {
		return nil, 0, err
	}
	for _, u := range list {
		u.Phone = maskPhone(u.Phone)
	}
	return list, total, nil
}

func maskPhone(p string) string {
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

// applyMembershipFilter 按会员状态筛选。
// member=有效会员；none=当前非会员（从未开通、已过期或等级停用）；
// expired 与 disabled 用于更细的运营筛选。
func applyMembershipFilter(db *gorm.DB, state string) *gorm.DB {
	if state == "" || state == "all" {
		return db
	}
	now := time.Now()
	enabledLevels := func() *gorm.DB {
		return db.Session(&gorm.Session{NewDB: true}).Model(&model.MembershipLevel{}).
			Select("id").Where("status = ? AND deleted_at IS NULL", model.StatusEnabled)
	}
	activeCond := "membership_level_id <> '' AND (membership_expire_at IS NULL OR membership_expire_at > ?) AND membership_level_id IN (?)"
	switch state {
	case "member":
		return db.Where(activeCond, now, enabledLevels())
	case "none":
		// 与 member 互补，保证两个筛选的并集等于全量用户。
		return db.Where("NOT ("+activeCond+")", now, enabledLevels())
	case "expired":
		return db.Where("membership_level_id <> '' AND membership_expire_at IS NOT NULL AND membership_expire_at <= ?", now)
	case "disabled":
		return db.Where("membership_level_id <> '' AND membership_level_id NOT IN (?)", enabledLevels())
	}
	return db
}

// attachRelations 批量填充角色与部门，避免 N+1 查询。
func (s *Service) attachRelations(ctx context.Context, users []*model.User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, 0, len(users))
	deptIDs := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
		if u.DeptID != "" {
			deptIDs = append(deptIDs, u.DeptID)
		}
	}
	type urRow struct {
		UserID    string
		ID        string
		Code      string
		Name      string
		DataScope int8
	}
	var rows []urRow
	if err := s.db.WithContext(ctx).Table("sys_user_role AS ur").
		Select("ur.user_id, r.id, r.code, r.name, r.data_scope").
		Joins("JOIN sys_role AS r ON r.id = ur.role_id AND r.deleted_at IS NULL").
		Where("ur.user_id IN ?", ids).Scan(&rows).Error; err != nil {
		return err
	}
	roleMap := map[string][]model.Role{}
	idMap := map[string][]string{}
	for _, r := range rows {
		roleMap[r.UserID] = append(roleMap[r.UserID], model.Role{
			Base: model.Base{ID: r.ID}, Code: r.Code, Name: r.Name, DataScope: r.DataScope,
		})
		idMap[r.UserID] = append(idMap[r.UserID], r.ID)
	}
	depts := map[string]*model.Dept{}
	if len(deptIDs) > 0 {
		var list []*model.Dept
		if err := s.db.WithContext(ctx).Where("id IN ?", deptIDs).Find(&list).Error; err != nil {
			return err
		}
		for _, d := range list {
			depts[d.ID] = d
		}
	}
	for _, u := range users {
		u.Roles = roleMap[u.ID]
		u.RoleIDs = idMap[u.ID]
		if d, ok := depts[u.DeptID]; ok {
			u.Dept = d
		}
	}
	if s.mem != nil {
		return s.mem.Fill(ctx, users)
	}
	return nil
}

// Get 查询单个用户。
func (s *Service) Get(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("用户不存在")
		}
		return nil, err
	}
	one := []*model.User{&u}
	if err := s.attachRelations(ctx, one); err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateInput 创建用户入参。
type CreateInput struct {
	Username string   `json:"username" binding:"required,username" label:"用户名"`
	Password string   `json:"password" binding:"required,min=8,max=64" label:"密码"`
	Nickname string   `json:"nickname" binding:"required,max=64" label:"昵称"`
	RealName string   `json:"realName" binding:"max=64" label:"姓名"`
	Email    string   `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`
	Phone    string   `json:"phone" binding:"omitempty,mobile" label:"手机号"`
	DeptID   string   `json:"deptId" label:"部门"`
	RoleIDs  []string `json:"roleIds" label:"角色"`
	Gender   int8     `json:"gender" binding:"oneof=0 1 2" label:"性别"`
	// Status 用指针区分"未传"与"显式禁用"：int8 的零值会让禁用账号被静默改成启用。
	Status *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Remark string `json:"remark" binding:"max=255" label:"备注"`
}

// Create 创建用户。
func (s *Service) Create(ctx context.Context, in *CreateInput) (*model.User, error) {
	var count int64
	s.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", in.Username).Count(&count)
	if count > 0 {
		return nil, errs.Conflict("用户名已存在")
	}
	if in.Phone != "" {
		s.db.WithContext(ctx).Model(&model.User{}).Where("phone = ?", in.Phone).Count(&count)
		if count > 0 {
			return nil, errs.Conflict("手机号已被使用")
		}
	}
	if err := checkStrength(ctx, in.Password, s.cfg.Security.PasswordMinLen, s.cfg.Security.PasswordStrength); err != nil {
		return nil, err
	}
	hash, err := authn.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	status := model.StatusEnabled
	if in.Status != nil {
		status = *in.Status
	}
	u := &model.User{
		Username: in.Username, Password: hash, Nickname: in.Nickname, RealName: in.RealName,
		Email: in.Email, Phone: in.Phone, DeptID: in.DeptID, Gender: in.Gender,
		Status: status, Remark: in.Remark,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("*").Create(u).Error; err != nil {
			if isUniqueViolation(err) {
				return errs.Conflict("用户名已存在")
			}
			return err
		}
		return replaceRoles(tx, u.ID, in.RoleIDs)
	})
	if err != nil {
		return nil, err
	}
	s.authz.Invalidate(ctx)
	return u, nil
}

// UpdateInput 更新用户入参。
type UpdateInput struct {
	Nickname string   `json:"nickname" binding:"required,max=64" label:"昵称"`
	RealName string   `json:"realName" binding:"max=64" label:"姓名"`
	Email    string   `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`
	Phone    string   `json:"phone" binding:"omitempty,mobile" label:"手机号"`
	DeptID   string   `json:"deptId" label:"部门"`
	RoleIDs  []string `json:"roleIds" label:"角色"`
	Gender   int8     `json:"gender" binding:"oneof=0 1 2" label:"性别"`
	Remark   string   `json:"remark" binding:"max=255" label:"备注"`
}

// Update 更新用户资料与角色。
func (s *Service) Update(ctx context.Context, id string, in *UpdateInput) error {
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		return errs.NotFound("用户不存在")
	}
	if in.Phone != "" && in.Phone != u.Phone {
		var n int64
		s.db.WithContext(ctx).Model(&model.User{}).Where("phone = ? AND id <> ?", in.Phone, id).Count(&n)
		if n > 0 {
			return errs.Conflict("手机号已被使用")
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", id).Updates(map[string]any{
			"nickname": in.Nickname, "real_name": in.RealName, "email": in.Email,
			"phone": in.Phone, "dept_id": in.DeptID, "gender": in.Gender, "remark": in.Remark,
		}).Error; err != nil {
			return err
		}
		if in.RoleIDs != nil {
			return replaceRoles(tx, id, in.RoleIDs)
		}
		return nil
	})
	if err == nil {
		s.authz.Invalidate(ctx)
	}
	return err
}

func replaceRoles(tx *gorm.DB, userID string, roleIDs []string) error {
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	for _, rid := range roleIDs {
		if rid == "" {
			continue
		}
		if err := tx.Create(&model.UserRole{UserID: userID, RoleID: rid}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Delete 删除用户（禁止删除自己与内置管理员）。
func (s *Service) Delete(ctx context.Context, id, operatorID string) error {
	if id == operatorID {
		return errs.BadRequest("不能删除当前登录账号")
	}
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		return errs.NotFound("用户不存在")
	}
	if u.Username == "admin" {
		return errs.Forbidden("内置管理员不可删除")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		// username 是唯一索引，软删除后仍会占用，导致同名账号无法重建；
		// 删除时改写为「原名#deleted#ID」，既保留历史记录又释放唯一键。
		released := releaseUsername(u.Username, id)
		if err := tx.Model(&model.User{}).Where("id = ?", id).
			Update("username", released).Error; err != nil {
			return err
		}
		return tx.Delete(&model.User{}, "id = ?", id).Error
	})
	if err == nil {
		s.authz.Invalidate(ctx)
	}
	return err
}

// releaseUsername 生成不与现存账号冲突的占位用户名，长度上限 64。
func releaseUsername(name, id string) string {
	const suffixTag = "#deleted#"
	suffix := suffixTag + id
	limit := 64 - len(suffix)
	if len(name) > limit {
		name = name[:limit]
	}
	return name + suffix
}

// isUniqueViolation 判断是否为唯一约束冲突，避免把数据库原文暴露给前端。
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ResetPassword 管理员重置密码，同时踢下线所有会话。
func (s *Service) ResetPassword(ctx context.Context, id, password string) error {
	if err := checkStrength(ctx, password, s.cfg.Security.PasswordMinLen, s.cfg.Security.PasswordStrength); err != nil {
		return err
	}
	hash, err := authn.HashPassword(password)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Update("password", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("用户不存在")
	}
	s.db.WithContext(ctx).Model(&model.Session{}).Where("user_id = ?", id).Update("revoked", true)
	return nil
}

// ResetTOTP 管理员重置指定用户的两步验证配置。
// 用于员工更换手机、认证器被卸载等无法自助关闭的场景；
// 重置后该用户所有会话失效，需要用密码重新登录并重新绑定。
func (s *Service) ResetTOTP(ctx context.Context, id, operatorID string) error {
	if id == operatorID {
		return errs.BadRequest("请在个人中心关闭自己的两步验证")
	}
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.NotFound("用户不存在")
		}
		return err
	}
	if !u.TOTPEnabled {
		return errs.BadRequest("该用户未开启两步验证")
	}
	// 超管是最後的兜底通道，不能被其他管理员顺手关掉二次验证
	if u.IsSuper {
		return errs.Forbidden("不能重置超级管理员的两步验证")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{"totp_enabled": false, "totp_secret": ""}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserRecoveryCode{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).Where("user_id = ?", id).Update("revoked", true).Error
	})
}

// SetStatus 启用/禁用用户。
func (s *Service) SetStatus(ctx context.Context, id string, status int8, operatorID string) error {
	if id == operatorID && status == model.StatusDisabled {
		return errs.BadRequest("不能禁用当前登录账号")
	}
	if status != 0 && status != 1 {
		return errs.BadRequest("状态值非法")
	}
	res := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("用户不存在")
	}
	if status == 0 {
		s.db.WithContext(ctx).Model(&model.Session{}).Where("user_id = ?", id).Update("revoked", true)
	}
	s.authz.Invalidate(ctx)
	return nil
}

// SetRoles 分配角色。
func (s *Service) SetRoles(ctx context.Context, id string, roleIDs []string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return errs.NotFound("用户不存在")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceRoles(tx, id, roleIDs)
	}); err != nil {
		return err
	}
	s.authz.Invalidate(ctx)
	return nil
}

// Option 下拉选项。
type Option struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	DeptID   string `json:"deptId,omitempty"`
}

// Options 返回用户下拉选项。
func (s *Service) Options(ctx context.Context, keyword string, limit int) ([]Option, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	db := s.db.WithContext(ctx).Model(&model.User{}).Where("status = ?", model.StatusEnabled)
	db = query.Search(db, keyword, "username", "nickname", "phone")
	var out []Option
	if err := db.Select("id, username, nickname, dept_id").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Export 导出用户列表为 Excel，筛选条件与列表保持一致。
func (s *Service) Export(ctx context.Context, p *query.Params, sc Scope, deptFilter []string) (*excelize.File, error) {
	exportParams := *p
	exportParams.Page = 1
	exportParams.PageSize = 5000
	list, _, err := s.List(ctx, &exportParams, sc, deptFilter)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	sheet := "用户列表"
	index, _ := f.NewSheet(sheet)
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")
	headers := []string{"ID", "用户名", "昵称", "姓名", "手机号", "邮箱", "部门", "角色", "会员等级", "会员到期", "状态", "最后登录", "创建时间"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EEF2F7"}},
	})
	lastCol, _ := excelize.CoordinatesToCellName(len(headers), 1)
	f.SetCellStyle(sheet, "A1", lastCol, style)
	for i, u := range list {
		row := i + 2
		roleNames := make([]string, 0, len(u.Roles))
		for _, r := range u.Roles {
			roleNames = append(roleNames, r.Name)
		}
		dept := ""
		if u.Dept != nil {
			dept = u.Dept.Name
		}
		status := "启用"
		if u.Status == 0 {
			status = "禁用"
		}
		level, expire := "非会员", ""
		if u.Membership != nil {
			level = u.Membership.Name
			if u.MembershipState == model.MembershipExpired {
				level += "（已过期）"
			}
			if u.MembershipExpireAt != nil {
				expire = u.MembershipExpireAt.Format("2006-01-02")
			}
		}
		last := ""
		if u.LastLoginAt != nil {
			last = u.LastLoginAt.Format("2006-01-02 15:04:05")
		}
		values := []any{u.ID, u.Username, u.Nickname, u.RealName, u.Phone, u.Email, dept,
			strings.Join(roleNames, "/"), level, expire, status, last, u.CreatedAt.Format("2006-01-02 15:04:05")}
		for j, v := range values {
			cell, _ := excelize.CoordinatesToCellName(j+1, row)
			f.SetCellValue(sheet, cell, v)
		}
	}
	f.SetColWidth(sheet, "A", "A", 22)
	f.SetColWidth(sheet, "B", "D", 16)
	f.SetColWidth(sheet, "G", "H", 18)
	f.SetColWidth(sheet, "I", "J", 14)
	return f, nil
}

func checkStrength(ctx context.Context, pw string, minLen, level int) error {
	if minLen < 8 {
		minLen = 8
	}
	if len([]rune(pw)) < minLen {
		return errs.BadRequest(i18n.Tf(ctx, "密码长度至少 {0} 位", minLen))
	}
	if level <= 1 {
		return nil
	}
	var upper, lower, digit, special bool
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			special = true
		}
	}
	if level >= 2 && (!digit || !(upper || lower)) {
		return errs.BadRequest("密码需同时包含字母与数字")
	}
	if level >= 3 && !(upper && lower && digit && special) {
		return errs.BadRequest("密码需包含大写字母、小写字母、数字与特殊符号")
	}
	return nil
}

// TouchLogin 记录登录时间（供第三方登录扩展使用）。
func (s *Service) TouchLogin(ctx context.Context, id, ip string) error {
	return s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(map[string]any{
		"last_login_at": time.Now(), "last_login_ip": ip, "login_count": gorm.Expr("login_count + 1"),
	}).Error
}
