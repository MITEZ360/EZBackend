package maintenance

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/migrate"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 备份与迁移管理接口。
type Handler struct {
	backup *BackupService
	runner *migrate.Runner
	mode   string
}

// NewHandler 创建运维处理器；runner 为空表示当前未启用版本化迁移。
func NewHandler(b *BackupService, runner *migrate.Runner, mode string) *Handler {
	return &Handler{backup: b, runner: runner, mode: mode}
}

// Backups 备份文件列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Backups
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups [get]
// KE:SWAGGER-END
func (h *Handler) Backups(c *gin.Context) {
	list, err := h.backup.List()
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("服务内部错误，请联系管理员"))
		return
	}
	response.OK(c, list)
}

// CreateBackup 立即备份。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary CreateBackup
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups [post]
// KE:SWAGGER-END
func (h *Handler) CreateBackup(c *gin.Context) {
	meta, err := h.backup.Backup(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("服务内部错误，请联系管理员"))
		return
	}
	middleware.AuditInfo(c, "运维", "立即备份数据库")
	response.OK(c, meta)
}

// BackupMeta 读取备份元信息。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary BackupMeta
// @Tags 运维
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups/{name}/meta [get]
// KE:SWAGGER-END
func (h *Handler) BackupMeta(c *gin.Context) {
	meta, err := h.backup.Meta(c.Param("name"))
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.NotFound("资源不存在或已被删除"))
		return
	}
	response.OK(c, meta)
}

// DownloadBackup 下载备份文件。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DownloadBackup
// @Tags 运维
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups/{name}/download [get]
// KE:SWAGGER-END
func (h *Handler) DownloadBackup(c *gin.Context) {
	p, err := h.backup.Path(c.Param("name"))
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.NotFound("资源不存在或已被删除"))
		return
	}
	middleware.AuditInfo(c, "运维", "下载数据库备份")
	c.FileAttachment(p, c.Param("name"))
}

// VerifyBackup 校验备份文件。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary VerifyBackup
// @Tags 运维
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups/{name}/verify [post]
// KE:SWAGGER-END
func (h *Handler) VerifyBackup(c *gin.Context) {
	res, err := h.backup.Verify(c.Param("name"))
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.BadRequest("操作失败，请检查输入或联系管理员"))
		return
	}
	response.OK(c, res)
}

// RestoreBackup 在线恢复（受 backup.allow_restore 控制）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary RestoreBackup
// @Tags 运维
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups/{name}/restore [post]
// KE:SWAGGER-END
func (h *Handler) RestoreBackup(c *gin.Context) {
	// dry_run=true 只返回恢复计划，不改动任何文件（SQLite 停机恢复也可安全演练）
	if strings.EqualFold(c.Query("dry_run"), "true") || c.Query("dry_run") == "1" {
		res, err := h.backup.RestorePlan(c.Param("name"))
		if err != nil {
			response.Fail(c, errs.BadRequest(err.Error()))
			return
		}
		response.OK(c, res)
		return
	}
	res, err := h.backup.RestoreOnline(c.Request.Context(), c.Param("name"))
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.BadRequest("操作失败，请检查输入或联系管理员"))
		return
	}
	middleware.AuditInfo(c, "运维", "在线恢复数据库")
	response.OK(c, res)
}

// PrunePlan 备份保留策略演练：返回将保留/删除的集合，不改动文件。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary PrunePlan
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/prune-plan [get]
// KE:SWAGGER-END
func (h *Handler) PrunePlan(c *gin.Context) {
	plan, err := h.backup.PruneDryRun()
	if err != nil {
		response.Fail(c, errs.Internal(err.Error()))
		return
	}
	response.OK(c, plan)
}

// DeleteBackup 删除备份。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DeleteBackup
// @Tags 运维
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/backups/{name} [delete]
// KE:SWAGGER-END
func (h *Handler) DeleteBackup(c *gin.Context) {
	if err := h.backup.Delete(c.Param("name")); err != nil {
		c.Error(err)
		response.Fail(c, errs.NotFound("资源不存在或已被删除"))
		return
	}
	middleware.AuditInfo(c, "运维", "删除数据库备份")
	response.OK(c, gin.H{"message": "备份已删除"})
}

// MigrationStatus 查看版本化迁移状态。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary MigrationStatus
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/migrations [get]
// KE:SWAGGER-END
func (h *Handler) MigrationStatus(c *gin.Context) {
	if h.runner == nil {
		response.OK(c, gin.H{"mode": h.mode, "enabled": false,
			"message": "当前使用 migration.mode=" + h.mode + "，未启用版本化迁移"})
		return
	}
	st, err := h.runner.Status(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("服务内部错误，请联系管理员"))
		return
	}
	response.OK(c, gin.H{"mode": h.mode, "enabled": true, "status": st})
}

// MigrateUp 手动升级到最新版本。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary MigrateUp
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/migrations/up [post]
// KE:SWAGGER-END
func (h *Handler) MigrateUp(c *gin.Context) {
	if h.runner == nil {
		response.Fail(c, errs.BadRequest("当前未启用版本化迁移"))
		return
	}
	applied, err := h.runner.Up(context.Background())
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("服务内部错误，请联系管理员"))
		return
	}
	middleware.AuditInfo(c, "运维", "执行数据库版本升级")
	response.OK(c, gin.H{"applied": applied})
}

// MigrateDown 回滚最近一个版本（高危，仅超管可用）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary MigrateDown
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/maintenance/migrations/down [post]
// KE:SWAGGER-END
func (h *Handler) MigrateDown(c *gin.Context) {
	if h.runner == nil {
		response.Fail(c, errs.BadRequest("当前未启用版本化迁移"))
		return
	}
	applied, err := h.runner.Down(context.Background())
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("服务内部错误，请联系管理员"))
		return
	}
	middleware.AuditInfo(c, "运维", "回滚数据库版本")
	response.OK(c, gin.H{"applied": applied})
}
