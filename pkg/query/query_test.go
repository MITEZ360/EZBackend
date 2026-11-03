package query

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sampleUser struct {
	ID       uint   `gorm:"primaryKey"`
	Name     string `gorm:"column:name"`
	Status   int    `gorm:"column:status"`
	DeptID   string `gorm:"column:dept_id"`
	CreateAt time.Time
}

func (sampleUser) TableName() string { return "sample_user" }

func newCtx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return c
}

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	if err := db.AutoMigrate(&sampleUser{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	rows := []sampleUser{
		{Name: "alice", Status: 1, DeptID: "d1", CreateAt: time.Now()},
		{Name: "bob", Status: 0, DeptID: "d1", CreateAt: time.Now()},
		{Name: "carol", Status: 1, DeptID: "d2", CreateAt: time.Now()},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("初始化数据失败: %v", err)
	}
	return db
}

func TestParse(t *testing.T) {
	p := Parse(newCtx("page=2&page_size=10&sort=-name&keyword=alice&status=1"))
	if p.Page != 2 || p.PageSize != 10 || p.Sort != "-name" || p.Keyword != "alice" {
		t.Fatalf("解析结果异常: %+v", p)
	}
	if p.Offset() != 10 {
		t.Fatalf("偏移量错误: %d", p.Offset())
	}
}

func TestParseDefaultsAndLimits(t *testing.T) {
	p := Parse(newCtx(""))
	if p.Page != 1 || p.PageSize != 20 {
		t.Fatalf("默认值错误: %+v", p)
	}
	p = Parse(newCtx("page=-3&page_size=10000"))
	if p.Page != 1 || p.PageSize != 500 {
		t.Fatalf("越界值未纠正: %+v", p)
	}
}

func TestOrder(t *testing.T) {
	allowed := map[string]string{"name": "name", "status": "status"}
	if got := Parse(newCtx("")).Order(allowed, "id DESC"); got != "id DESC" {
		t.Fatalf("默认排序错误: %s", got)
	}
	if got := Parse(newCtx("sort=-name,status")).Order(allowed, "id DESC"); got != "name DESC, status ASC" {
		t.Fatalf("多字段排序错误: %s", got)
	}
	// 非白名单字段必须被忽略，防止 SQL 注入
	if got := Parse(newCtx("sort=password")).Order(allowed, "id DESC"); got != "id DESC" {
		t.Fatalf("非法排序字段未被拦截: %s", got)
	}
}

func TestFilter(t *testing.T) {
	db := newDB(t)
	cases := []struct {
		q    string
		want int64
	}{
		{"status=1", 2},
		{"dept_id=d1", 2},
		{"deptId=d1", 2},      // camelCase 应归一化为 dept_id
		{"deptId_in=d1,d2", 3}, // 后缀与 camelCase 组合
		{"name_like=li", 1},
		{"name_prefix=ca", 1},
		{"status_in=0,1", 3},
		{"id_gte=2", 2},
		{"id_lt=2", 1},
		{"name_ne=alice", 2},
		{"unknown=1", 3}, // 非白名单参数被忽略
	}
	allowed := map[string]string{"name": "name", "status": "status", "dept_id": "dept_id", "id": "id"}
	for _, c := range cases {
		var n int64
		q := Parse(newCtx(c.q)).Filter(db.Model(&sampleUser{}), allowed)
		if err := q.Count(&n).Error; err != nil {
			t.Fatalf("查询 %s 失败: %v", c.q, err)
		}
		if n != c.want {
			t.Fatalf("过滤 %s 期望 %d 条，实际 %d", c.q, c.want, n)
		}
	}
}

func TestSearch(t *testing.T) {
	db := newDB(t)
	var n int64
	if err := Search(db.Model(&sampleUser{}), "ali", "name").Count(&n).Error; err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("搜索结果数错误: %d", n)
	}
	var all int64
	_ = Search(db.Model(&sampleUser{}), "", "name").Count(&all).Error
	if all != 3 {
		t.Fatalf("空关键字应返回全部: %d", all)
	}
}

func TestSearchEscape(t *testing.T) {
	db := newDB(t)
	var n int64
	// 关键字含 % 时必须按字面匹配，不能变成通配符
	_ = Search(db.Model(&sampleUser{}), "%", "name").Count(&n).Error
	if n != 0 {
		t.Fatalf("通配符未转义，命中 %d 条", n)
	}
}
