// Package logger 基于 Zap 提供结构化日志：JSON 落盘 + 控制台彩色输出 + 按大小/日期轮转 +
// 采样限流，并提供 GORM 慢查询日志适配器。
package logger

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

// Options 日志构建参数。
type Options struct {
	Config  config.Log
	Service string
	Env     string
	Version string
}

// level 保存当前日志级别，供管理端运行期调整。
//
// zap 的 AtomicLevel 会同步更新所有引用它的 core，因此不需要重建 logger。
var level = zap.NewAtomicLevelAt(zapcore.InfoLevel)

// SetLevel 运行期调整日志级别，返回 false 表示级别名非法。
func SetLevel(s string) bool {
	l, err := zapcore.ParseLevel(s)
	if err != nil {
		return false
	}
	level.SetLevel(l)
	return true
}

// Level 返回当前日志级别名。
func Level() string { return level.Level().String() }

// New 创建日志器。文件输出为 JSON（便于采集），控制台按 format 决定是否彩色。
func New(o Options) (*zap.Logger, error) {
	// 复用全局 AtomicLevel：管理端热改级别后，重建 logger 也不会丢设置。
	lvl := level
	lvl.SetLevel(parseLevel(o.Config.Level))

	consoleEnc := newEncoder(o.Config.Format == "console")
	fileEnc := newEncoder(false)

	var cores []zapcore.Core
	consoleEnabled := o.Config.Console
	if consoleEnabled {
		cores = append(cores, zapcore.NewCore(consoleEnc, zapcore.Lock(os.Stdout), lvl))
	}
	if o.Config.Dir != "" {
		if err := os.MkdirAll(o.Config.Dir, 0o755); err != nil {
			return nil, err
		}
		name := o.Config.Filename
		if name == "" {
			name = "tenon.log"
		}
		w := &lumberjack.Logger{
			Filename:   filepath.Join(o.Config.Dir, name),
			MaxSize:    orDefault(o.Config.MaxSizeMB, 64),
			MaxAge:     orDefault(o.Config.MaxAgeDays, 14),
			MaxBackups: orDefault(o.Config.MaxBackups, 30),
			Compress:   o.Config.Compress,
			LocalTime:  true,
		}
		cores = append(cores, zapcore.NewCore(fileEnc, zapcore.AddSync(w), lvl))
	}
	if len(cores) == 0 {
		cores = append(cores, zapcore.NewCore(consoleEnc, zapcore.Lock(os.Stdout), lvl))
	}

	core := zapcore.NewTee(cores...)
	// 采样：每秒前 100 条全量，之后同类日志每 100 条保留 1 条，防止突发流量打爆磁盘
	core = zapcore.NewSamplerWithOptions(core, time.Second, 100, 100)

	opts := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(0),
		zap.Fields(
			zap.String("service", o.Service),
			zap.String("env", o.Env),
			zap.String("version", o.Version),
		),
	}
	if !o.Config.Caller {
		opts = []zap.Option{zap.Fields(zap.String("service", o.Service), zap.String("env", o.Env)), zap.AddCallerSkip(0)}
	}
	return zap.New(core, opts...), nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func parseLevel(s string) zapcore.Level {
	l, err := zapcore.ParseLevel(s)
	if err != nil {
		return zapcore.InfoLevel
	}
	return l
}

func newEncoder(console bool) zapcore.Encoder {
	encCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if console {
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("15:04:05.000"))
		}
		return zapcore.NewConsoleEncoder(encCfg)
	}
	return zapcore.NewJSONEncoder(encCfg)
}

// Gorm 返回适配 Zap 的 GORM 日志器，支持慢 SQL 告警与错误记录。
func Gorm(z *zap.Logger, db config.DB) gormlogger.Interface {
	level := gormlogger.Warn
	switch db.LogLevel {
	case "silent":
		level = gormlogger.Silent
	case "error":
		level = gormlogger.Error
	case "info":
		level = gormlogger.Info
	}
	slow := db.SlowThreshold
	if slow <= 0 {
		slow = 200 * time.Millisecond
	}
	return &gormLogger{zap: z.Named("gorm"), level: level, slow: slow}
}

type gormLogger struct {
	zap   *zap.Logger
	level gormlogger.LogLevel
	slow  time.Duration
}

func (g *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	n := *g
	n.level = level
	return &n
}

func (g *gormLogger) Info(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Info {
		g.zap.Sugar().Infof(msg, args...)
	}
}

func (g *gormLogger) Warn(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Warn {
		g.zap.Sugar().Warnf(msg, args...)
	}
}

func (g *gormLogger) Error(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Error {
		g.zap.Sugar().Errorf(msg, args...)
	}
}

func (g *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if g.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	switch {
	case err != nil && err != gorm.ErrRecordNotFound && g.level >= gormlogger.Error:
		g.zap.Error("SQL 执行失败",
			zap.Error(err),
			zap.Duration("elapsed", elapsed),
			zap.Int64("rows", rows),
			zap.String("sql", sql),
		)
	case elapsed > g.slow && g.level >= gormlogger.Warn:
		g.zap.Warn("慢 SQL",
			zap.Duration("elapsed", elapsed),
			zap.Int64("rows", rows),
			zap.String("sql", sql),
		)
	case g.level >= gormlogger.Info:
		g.zap.Debug("SQL",
			zap.Duration("elapsed", elapsed),
			zap.Int64("rows", rows),
			zap.String("sql", sql),
		)
	}
}

// StackField 返回当前调用栈字符串，供 panic 日志使用。
func StackField() zap.Field {
	buf := make([]byte, 8<<10)
	n := runtime.Stack(buf, false)
	return zap.String("stack", string(buf[:n]))
}

// Fields 是日志字段的简写集合。
type Fields = []zap.Field

// F 构造单个字段。
func F(key string, val any) zap.Field {
	switch v := val.(type) {
	case string:
		return zap.String(key, v)
	case int:
		return zap.Int(key, v)
	case int64:
		return zap.Int64(key, v)
	case float64:
		return zap.Float64(key, v)
	case bool:
		return zap.Bool(key, v)
	case time.Duration:
		return zap.Duration(key, v)
	case error:
		return zap.Error(v)
	default:
		return zap.Any(key, val)
	}
}

// Str 构造字符串字段。
func Str(key, val string) zap.Field { return zap.String(key, val) }

// Int 构造整型字段。
func Int(key string, val int) zap.Field { return zap.Int(key, val) }

// Err 构造错误字段。
func Err(err error) zap.Field { return zap.Error(err) }
