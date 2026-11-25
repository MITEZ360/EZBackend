package recharge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/crypto"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
)

// maskSecret 是管理端读回敏感字段时的占位符；保存时遇到该值表示保持原值。
const maskSecret = "******"

// WechatPayView 管理端微信支付配置。
type WechatPayView struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"appId"`
	MchID     string `json:"mchId"`
	APIKey    string `json:"apiKey"`
	NotifyURL string `json:"notifyUrl"`
	SignType  string `json:"signType"`
	Sandbox   bool   `json:"sandbox"`
}

// AlipayPayView 管理端支付宝配置。
type AlipayPayView struct {
	Enabled    bool   `json:"enabled"`
	AppID      string `json:"appId"`
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	NotifyURL  string `json:"notifyUrl"`
	ReturnURL  string `json:"returnUrl"`
	Sandbox    bool   `json:"sandbox"`
}

// PaymentConfigView 管理端支付配置视图，同时用作保存入参。
// Source 为 file 表示尚未在管理端保存过，当前生效的是启动配置文件。
type PaymentConfigView struct {
	Enabled bool          `json:"enabled"`
	Wechat  WechatPayView `json:"wechat"`
	Alipay  AlipayPayView `json:"alipay"`
	Source  string        `json:"source,omitempty"`
}

// PaymentConfigStore 管理端支付配置存储。
// 数据库中保存记录时覆盖启动配置，未保存时回退到 config.cn.yaml / 环境变量，
// 因此现有部署无需改动即可继续工作。
type PaymentConfigStore struct {
	db         *gorm.DB
	cfg        *config.Config
	tableReady bool
}

func newPaymentConfigStore(db *gorm.DB, cfg *config.Config) *PaymentConfigStore {
	ready := db != nil && db.Migrator().HasTable(&model.PaymentConfig{})
	return &PaymentConfigStore{db: db, cfg: cfg, tableReady: ready}
}

// Snapshot 返回当前生效的支付配置。每次调用都读库，保证管理端保存后立即生效。
func (s *PaymentConfigStore) Snapshot(ctx context.Context) (config.Payment, error) {
	out := s.cfg.Payment
	if !s.tableReady {
		return out, nil
	}
	var rows []model.PaymentConfig
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return out, err
	}
	fromDB := false
	for i := range rows {
		row := &rows[i]
		fromDB = true
		if row.Channel == model.PayConfigGlobal {
			out.Enabled = row.Enabled
			continue
		}
		plain, err := crypto.Decrypt(row.Payload, s.cfg.Security.DataEncryptKey)
		if err != nil {
			return out, fmt.Errorf("解密支付配置失败: %w", err)
		}
		switch row.Channel {
		case model.PayWechat:
			var c config.WechatPay
			if err := json.Unmarshal([]byte(plain), &c); err != nil {
				return out, err
			}
			c.Enabled = row.Enabled
			out.Wechat = c
		case model.PayAlipay:
			var c config.AlipayPay
			if err := json.Unmarshal([]byte(plain), &c); err != nil {
				return out, err
			}
			c.Enabled = row.Enabled
			out.Alipay = c
		}
	}
	if !fromDB {
		out.Enabled = s.cfg.Payment.Enabled
	}
	return out, nil
}

// View 返回管理端视图，敏感字段以 ****** 占位。
func (s *PaymentConfigStore) View(ctx context.Context) (*PaymentConfigView, error) {
	p, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	view := &PaymentConfigView{
		Enabled: p.Enabled,
		Wechat: WechatPayView{
			Enabled: p.Wechat.Enabled, AppID: p.Wechat.AppID, MchID: p.Wechat.MchID,
			APIKey: maskIfSet(p.Wechat.APIKey), NotifyURL: p.Wechat.NotifyURL,
			SignType: p.Wechat.SignType, Sandbox: p.Wechat.Sandbox,
		},
		Alipay: AlipayPayView{
			Enabled: p.Alipay.Enabled, AppID: p.Alipay.AppID,
			PrivateKey: maskIfSet(p.Alipay.PrivateKey), PublicKey: maskIfSet(p.Alipay.PublicKey),
			NotifyURL: p.Alipay.NotifyURL, ReturnURL: p.Alipay.ReturnURL, Sandbox: p.Alipay.Sandbox,
		},
		Source: "file",
	}
	if s.tableReady {
		var n int64
		_ = s.db.WithContext(ctx).Model(&model.PaymentConfig{}).Count(&n).Error
		if n > 0 {
			view.Source = "database"
		}
	}
	return view, nil
}

func maskIfSet(v string) string {
	if v == "" {
		return ""
	}
	return maskSecret
}

// Save 保存管理端配置。敏感字段传入 ****** 时保持数据库中原值不变。
func (s *PaymentConfigStore) Save(ctx context.Context, in *PaymentConfigView) error {
	if !s.tableReady {
		return errs.Internal(i18n.Tf(ctx, "支付配置表不存在，请先执行数据库迁移"))
	}
	cur, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	wechat := cur.Wechat
	wechat.Enabled = in.Wechat.Enabled
	wechat.AppID = in.Wechat.AppID
	wechat.MchID = in.Wechat.MchID
	wechat.NotifyURL = in.Wechat.NotifyURL
	wechat.SignType = in.Wechat.SignType
	wechat.Sandbox = in.Wechat.Sandbox
	if in.Wechat.APIKey != maskSecret {
		wechat.APIKey = in.Wechat.APIKey
	}

	alipay := cur.Alipay
	alipay.Enabled = in.Alipay.Enabled
	alipay.AppID = in.Alipay.AppID
	alipay.NotifyURL = in.Alipay.NotifyURL
	alipay.ReturnURL = in.Alipay.ReturnURL
	alipay.Sandbox = in.Alipay.Sandbox
	if in.Alipay.PrivateKey != maskSecret {
		alipay.PrivateKey = in.Alipay.PrivateKey
	}
	if in.Alipay.PublicKey != maskSecret {
		alipay.PublicKey = in.Alipay.PublicKey
	}

	wechatJSON, err := json.Marshal(wechat)
	if err != nil {
		return err
	}
	alipayJSON, err := json.Marshal(alipay)
	if err != nil {
		return err
	}
	rows := []model.PaymentConfig{
		{Channel: model.PayConfigGlobal, Enabled: in.Enabled, Payload: "{}"},
		{Channel: model.PayWechat, Enabled: wechat.Enabled, Payload: string(wechatJSON)},
		{Channel: model.PayAlipay, Enabled: alipay.Enabled, Payload: string(alipayJSON)},
	}
	for i := range rows {
		row := &rows[i]
		enc, e := crypto.Encrypt(row.Payload, s.cfg.Security.DataEncryptKey)
		if e != nil {
			return e
		}
		row.Payload = enc
		if e := s.upsert(ctx, row); e != nil {
			return e
		}
	}
	return nil
}

func (s *PaymentConfigStore) upsert(ctx context.Context, row *model.PaymentConfig) error {
	var existing model.PaymentConfig
	err := s.db.WithContext(ctx).Where("channel = ?", row.Channel).First(&existing).Error
	if err == nil {
		existing.Enabled = row.Enabled
		existing.Payload = row.Payload
		return s.db.WithContext(ctx).Save(&existing).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.db.WithContext(ctx).Create(row).Error
}
