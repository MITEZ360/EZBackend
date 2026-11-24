package user

import (
	"context"
	"io"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/i18n"
)

const (
	importMaxRows  = 1000
	importMaxError = 100
	importMaxSize  = 10 << 20
	importSheet    = "用户导入"
)

// 列顺序与模板、说明完全一致，调整时三处需同步。
var importHeaders = []string{"用户名*", "昵称*", "姓名", "手机号", "邮箱", "部门", "角色", "初始密码*", "状态"}

var importNotes = [][]string{
	{"字段", "是否必填", "规则说明"},
	{"用户名", "是", "字母开头，2-32 位，仅含字母、数字、下划线；全系统唯一"},
	{"昵称", "是", "界面展示名称，最长 64 字"},
	{"姓名", "否", "真实姓名，最长 64 字"},
	{"手机号", "否", "11 位大陆手机号；全系统唯一"},
	{"邮箱", "否", "合法邮箱地址，最长 128 字"},
	{"部门", "否", "填写部门名称，须与系统中已存在的部门完全一致"},
	{"角色", "否", "填写角色名称，多个角色用英文逗号分隔，须已存在"},
	{"初始密码", "是", "需满足系统密码策略（默认至少 8 位，含字母与数字）"},
	{"状态", "否", "启用 / 禁用，留空默认启用"},
	{"", "", ""},
	{"注意", "", "首行为表头请勿删除；单次最多导入 1000 行；导入不会覆盖已存在的用户名。"},
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{1,31}$`)
var mobilePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ImportFailure 单行失败明细。
type ImportFailure struct {
	Row      int    `json:"row"`
	Username string `json:"username,omitempty"`
	Message  string `json:"message"`
}

// ImportResult 导入结果。
type ImportResult struct {
	Total     int             `json:"total"`
	Success   int             `json:"success"`
	Failed    int             `json:"failed"`
	Truncated bool            `json:"truncated"`
	Errors    []ImportFailure `json:"errors"`
}

// ImportTemplate 生成导入模板：首个工作表用于填写，第二个工作表为字段规则。
func (s *Service) ImportTemplate() (*excelize.File, error) {
	f := excelize.NewFile()
	idx, err := f.NewSheet(importSheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	head, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#EEF2F7"}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	for i, h := range importHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(importSheet, cell, h)
	}
	_ = f.SetCellStyle(importSheet, "A1", "I1", head)
	_ = f.SetRowHeight(importSheet, 1, 26)
	for col, width := range []float64{18, 16, 14, 16, 24, 18, 22, 18, 10} {
		letter, _ := excelize.ColumnNumberToName(col + 1)
		_ = f.SetColWidth(importSheet, letter, letter, width)
	}
	if err := f.AutoFilter(importSheet, "A1:I1", nil); err != nil {
		return nil, err
	}

	noteSheet := "填写说明"
	if _, err := f.NewSheet(noteSheet); err != nil {
		return nil, err
	}
	for r, row := range importNotes {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			_ = f.SetCellValue(noteSheet, cell, v)
		}
	}
	_ = f.SetColWidth(noteSheet, "A", "A", 16)
	_ = f.SetColWidth(noteSheet, "B", "B", 12)
	_ = f.SetColWidth(noteSheet, "C", "C", 72)
	return f, nil
}

// Import 解析 Excel 并逐行创建用户，单行失败不影响其余行。
func (s *Service) Import(ctx context.Context, r io.Reader, size int64) (*ImportResult, error) {
	if size > importMaxSize {
		return nil, errs.BadRequest("导入文件不能超过 10MB")
	}
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, errs.BadRequest("无法解析文件，请使用 .xlsx 格式的模板")
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, errs.BadRequest("工作表读取失败")
	}
	defer rows.Close()

	res := &ImportResult{Errors: make([]ImportFailure, 0, 8)}
	rowNo := 0
	headerChecked := false

	for rows.Next() {
		rowNo++
		cols, err := rows.Columns()
		if err != nil {
			return nil, errs.BadRequest(i18n.Tf(ctx, "第 {0} 行读取失败", rowNo))
		}
		if !headerChecked {
			headerChecked = true
			if !looksLikeHeader(cols) {
				return nil, errs.BadRequest("表头不匹配，请下载最新模板后填写")
			}
			continue
		}
		if isBlankRow(cols) {
			continue
		}
		if res.Total >= importMaxRows {
			return nil, errs.BadRequest(i18n.Tf(ctx, "单次最多导入 {0} 行，请拆分后重试", importMaxRows))
		}
		res.Total++

		username := strings.TrimSpace(at(cols, 0))
		if msg := s.importOne(ctx, cols); msg != "" {
			res.Failed++
			if len(res.Errors) < importMaxError {
				res.Errors = append(res.Errors, ImportFailure{Row: rowNo, Username: username, Message: msg})
			} else {
				res.Truncated = true
			}
			continue
		}
		res.Success++
	}
	return res, nil
}

// importOne 校验并创建单行用户，返回空字符串表示成功。
func (s *Service) importOne(ctx context.Context, cols []string) string {
	in := &CreateInput{
		Username: strings.TrimSpace(at(cols, 0)),
		Nickname: strings.TrimSpace(at(cols, 1)),
		RealName: strings.TrimSpace(at(cols, 2)),
		Phone:    strings.TrimSpace(at(cols, 3)),
		Email:    strings.TrimSpace(at(cols, 4)),
		Password: strings.TrimSpace(at(cols, 7)),
	}

	if in.Username == "" {
		return "用户名不能为空"
	}
	if !usernamePattern.MatchString(in.Username) {
		return "用户名需字母开头、2-32 位，仅含字母、数字、下划线"
	}
	if in.Nickname == "" {
		return "昵称不能为空"
	}
	if in.Phone != "" && !mobilePattern.MatchString(in.Phone) {
		return "手机号格式不正确"
	}
	if in.Email != "" && (!emailPattern.MatchString(in.Email) || len(in.Email) > 128) {
		return "邮箱格式不正确"
	}
	if in.Password == "" {
		return "初始密码不能为空"
	}
	if err := checkStrength(ctx, in.Password, s.cfg.Security.PasswordMinLen, s.cfg.Security.PasswordStrength); err != nil {
		return err.Error()
	}

	deptID, err := s.resolveDept(ctx, strings.TrimSpace(at(cols, 5)))
	if err != nil {
		return err.Error()
	}
	in.DeptID = deptID

	roleIDs, err := s.resolveRoles(ctx, strings.TrimSpace(at(cols, 6)))
	if err != nil {
		return err.Error()
	}
	in.RoleIDs = roleIDs

	status, err := parseImportStatus(strings.TrimSpace(at(cols, 8)))
	if err != nil {
		return err.Error()
	}
	in.Status = &status

	if _, err := s.Create(ctx, in); err != nil {
		return err.Error()
	}
	return ""
}

// resolveDept 按名称定位部门；空值表示不设置。同名部门无法确定，直接报错。
func (s *Service) resolveDept(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	var list []model.Dept
	if err := s.db.WithContext(ctx).Where("name = ?", name).Limit(2).Find(&list).Error; err != nil {
		return "", errs.Internal("部门查询失败")
	}
	switch len(list) {
	case 0:
		return "", errs.BadRequest(i18n.Tf(ctx, "部门不存在：{0}", name))
	case 1:
		return list[0].ID, nil
	default:
		return "", errs.BadRequest(i18n.Tf(ctx, "存在多个同名部门，无法确定：{0}", name))
	}
}

// resolveRoles 按名称解析角色，支持中英文逗号、顿号与斜杠分隔。
func (s *Service) resolveRoles(ctx context.Context, raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == '/' || r == '|'
	})
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		var r model.Role
		err := s.db.WithContext(ctx).Where("name = ?", name).First(&r).Error
		if err != nil {
			return nil, errs.BadRequest(i18n.Tf(ctx, "角色不存在：{0}", name))
		}
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func parseImportStatus(v string) (int8, error) {
	switch strings.ToLower(v) {
	case "", "1", "启用", "正常", "enabled", "active":
		return model.StatusEnabled, nil
	case "0", "禁用", "停用", "disabled", "inactive":
		return model.StatusDisabled, nil
	default:
		return 0, errs.BadRequest("状态仅支持「启用」或「禁用」")
	}
}

// BatchDelete 批量删除，返回成功数量与被跳过的 ID（内置管理员、当前账号）。
func (s *Service) BatchDelete(ctx context.Context, ids []string, operatorID string) (int, []string) {
	deleted := 0
	skipped := make([]string, 0)
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.Delete(ctx, id, operatorID); err != nil {
			skipped = append(skipped, id)
			continue
		}
		deleted++
	}
	return deleted, skipped
}

func looksLikeHeader(cols []string) bool {
	first := strings.TrimSpace(at(cols, 0))
	return strings.HasPrefix(first, "用户名")
}

func isBlankRow(cols []string) bool {
	for _, c := range cols {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func at(cols []string, i int) string {
	if i >= 0 && i < len(cols) {
		return cols[i]
	}
	return ""
}
