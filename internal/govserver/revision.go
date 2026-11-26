package govserver

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/model"
)

// NextRev 在事务内自增全局治理版本并返回新值。
//
// 调用方必须已经在事务里：版本单调是数据面判断新旧的全部依据，
// 读-改-写分成两步会让两个并发发布拿到同一个 revision。
func NextRev(tx *gorm.DB) (int64, error) {
	res := tx.Model(&model.GovRevision{}).Where("id = 1").
		UpdateColumn("global_rev", gorm.Expr("global_rev + 1"))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model.GovRevision{ID: 1, GlobalRev: 1}).Error; err != nil {
			return 0, err
		}
	}
	var row model.GovRevision
	if err := tx.Where("id = 1").First(&row).Error; err != nil {
		return 0, err
	}
	return row.GlobalRev, nil
}
