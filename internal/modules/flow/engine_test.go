package flow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/model"
)

func newTestEngine(t *testing.T) (*Engine, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "flow.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserRole{}, &model.Dept{},
		&model.Flow{}, &model.FlowInstance{}, &model.FlowTask{}, &model.FlowRecord{}, &model.FlowDelegate{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return NewEngine(db, zap.NewNop()), db
}

func seedUsers(t *testing.T, db *gorm.DB) []model.User {
	t.Helper()
	users := []model.User{
		{Username: "alice", RealName: "爱丽丝", Status: model.StatusEnabled},
		{Username: "bob", RealName: "鲍勃", Status: model.StatusEnabled},
		{Username: "carol", RealName: " Carol", Status: model.StatusEnabled},
		{Username: "depthead", RealName: "部门负责人", Status: model.StatusEnabled},
	}
	for i := range users {
		want := users[i].Status
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("建用户失败: %v", err)
		}
		if want == model.StatusDisabled {
			db.Model(&users[i]).Update("status", model.StatusDisabled)
		}
	}
	return users
}

func newFlow(t *testing.T, db *gorm.DB, nodes []model.FlowNode) model.Flow {
	t.Helper()
	f := model.Flow{Key: "expense", Name: "费用报销", Status: model.FlowPublished, Nodes: nodes}
	if err := db.Create(&f).Error; err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	return f
}

func userNode(id, name, uid string) model.FlowNode {
	return model.FlowNode{ID: id, Name: name, Type: "approve", Mode: model.FlowModeAll,
		Assignees: []model.FlowAssignee{{Type: model.AssigneeUser, Value: uid}}}
}

func tasksOf(t *testing.T, db *gorm.DB, instID string) []model.FlowTask {
	t.Helper()
	var out []model.FlowTask
	db.Where("instance_id = ?", instID).Order("created_at, id").Find(&out)
	return out
}

func pendingCount(t *testing.T, db *gorm.DB, instID string) int {
	t.Helper()
	var n int64
	db.Model(&model.FlowTask{}).Where("instance_id = ? AND status = ?", instID, model.TaskPending).Count(&n)
	return int(n)
}

func TestSingleLine(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	newFlow(t, db, []model.FlowNode{
		userNode("n1", "主管", users[0].ID),
		userNode("n2", "经理", users[1].ID),
	})
	inst, err := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, db, inst.ID) != 1 {
		t.Fatalf("首节点应有 1 条待办, got %d", pendingCount(t, db, inst.ID))
	}
	first := tasksOf(t, db, inst.ID)[0]
	if err := e.HandleTask(context.Background(), first.ID, TaskInput{Action: model.TaskActApprove, Operator: first.AssigneeID}); err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.Status != model.FlowRunning || got.CurrentNode != "n2" {
		t.Fatalf("应推进到 n2, status=%s current=%s", got.Status, got.CurrentNode)
	}
	second := tasksOf(t, db, inst.ID)
	if len(second) != 2 || second[1].NodeID != "n2" {
		t.Fatalf("应新增 n2 任务, got %+v", second)
	}
	if err := e.HandleTask(context.Background(), second[1].ID, TaskInput{Action: model.TaskActApprove}); err != nil {
		t.Fatal(err)
	}
	db.First(&got, inst.ID)
	if got.Status != model.FlowApproved {
		t.Fatalf("应审批通过, got %s", got.Status)
	}
}

func TestAllModeWaitsForEveryone(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	n := model.FlowNode{ID: "n1", Name: "会签", Type: "approve", Mode: model.FlowModeAll,
		Assignees: []model.FlowAssignee{
			{Type: model.AssigneeUser, Value: users[0].ID},
			{Type: model.AssigneeUser, Value: users[1].ID},
		}}
	newFlow(t, db, []model.FlowNode{n, userNode("n2", "归档", users[2].ID)})
	inst, err := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, db, inst.ID) != 2 {
		t.Fatalf("会签应有 2 条待办, got %d", pendingCount(t, db, inst.ID))
	}
	first := tasksOf(t, db, inst.ID)[0]
	if err := e.HandleTask(context.Background(), first.ID, TaskInput{Action: model.TaskActApprove}); err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.CurrentNode != "n1" {
		t.Fatalf("会签未齐不应推进, current=%s", got.CurrentNode)
	}
	all := tasksOf(t, db, inst.ID)
	var secondID string
	for _, x := range all {
		if x.Status == model.TaskPending {
			secondID = x.ID
		}
	}
	if err := e.HandleTask(context.Background(), secondID, TaskInput{Action: model.TaskActApprove}); err != nil {
		t.Fatal(err)
	}
	db.First(&got, inst.ID)
	if got.CurrentNode != "n2" {
		t.Fatalf("会签齐了应推进, current=%s", got.CurrentNode)
	}
}

func TestAnyModeAdvancesOnFirst(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	n := model.FlowNode{ID: "n1", Name: "或签", Type: "approve", Mode: model.FlowModeAny,
		Assignees: []model.FlowAssignee{
			{Type: model.AssigneeUser, Value: users[0].ID},
			{Type: model.AssigneeUser, Value: users[1].ID},
		}}
	newFlow(t, db, []model.FlowNode{n})
	inst, _ := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	first := tasksOf(t, db, inst.ID)[0]
	if err := e.HandleTask(context.Background(), first.ID, TaskInput{Action: model.TaskActApprove}); err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, db, inst.ID) != 0 {
		t.Fatalf("或签通过后其余待办应取消, got %d", pendingCount(t, db, inst.ID))
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.Status != model.FlowApproved {
		t.Fatalf("应结束, got %s", got.Status)
	}
}

func TestSkipWhen(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	skip := userNode("n1", "大额审批", users[0].ID)
	skip.SkipWhen = &model.FlowCondition{Field: "amount", Op: "lte", Value: 5000}
	newFlow(t, db, []model.FlowNode{skip, userNode("n2", "常规审批", users[1].ID)})
	inst, err := e.Start(context.Background(), StartInput{
		FlowKey: "expense", InitiatorID: users[2].ID, Variables: map[string]any{"amount": 3000},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.CurrentNode != "n2" {
		t.Fatalf("应跳过 n1, current=%s", got.CurrentNode)
	}
}

func TestAddSignAndRemoveSign(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	newFlow(t, db, []model.FlowNode{userNode("n1", "审批", users[0].ID)})
	inst, _ := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	task := tasksOf(t, db, inst.ID)[0]
	if err := e.HandleTask(context.Background(), task.ID, TaskInput{
		Action: model.TaskActAddSign, TargetID: users[1].ID, Operator: users[0].ID}); err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, db, inst.ID) != 2 {
		t.Fatalf("加签后应有 2 条待办, got %d", pendingCount(t, db, inst.ID))
	}
	all := tasksOf(t, db, inst.ID)
	if err := e.HandleTask(context.Background(), all[0].ID, TaskInput{
		Action: model.TaskActApprove, Operator: all[0].AssigneeID}); err != nil {
		t.Fatal(err)
	}
	if pendingCount(t, db, inst.ID) != 1 {
		t.Fatalf("会签未齐应留 1 条待办, got %d", pendingCount(t, db, inst.ID))
	}
	var added model.FlowTask
	db.Where("is_added = ? AND status = ?", true, model.TaskPending).First(&added)
	if err := e.HandleTask(context.Background(), added.ID, TaskInput{
		Action: model.TaskActApprove, Operator: added.AssigneeID}); err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.Status != model.FlowApproved {
		t.Fatalf("加签全通过应结束, got %s", got.Status)
	}
}

func TestRollback(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	newFlow(t, db, []model.FlowNode{
		userNode("n1", "主管", users[0].ID),
		userNode("n2", "经理", users[1].ID),
		userNode("n3", "归档", users[2].ID),
	})
	inst, _ := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	first := tasksOf(t, db, inst.ID)[0]
	e.HandleTask(context.Background(), first.ID, TaskInput{Action: model.TaskActApprove})
	if err := e.Rollback(context.Background(), inst.ID, "n1", "材料不全", users[1].ID); err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.CurrentNode != "n1" || got.Round != 2 {
		t.Fatalf("应回退到 n1 且 round=2, current=%s round=%d", got.CurrentNode, got.Round)
	}
	if pendingCount(t, db, inst.ID) != 1 {
		t.Fatalf("回退后应只有 1 条新待办, got %d", pendingCount(t, db, inst.ID))
	}
	var recs []model.FlowRecord
	db.Where("instance_id = ? AND action = ?", inst.ID, model.TaskActRollback).Find(&recs)
	if len(recs) != 1 || recs[0].Comment != "材料不全" {
		t.Fatalf("回退未留痕: %+v", recs)
	}
}

func TestDelegate(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	now := time.Now()
	db.Create(&model.FlowDelegate{FromUserID: users[0].ID, ToUserID: users[1].ID,
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour), Enabled: true})
	newFlow(t, db, []model.FlowNode{userNode("n1", "主管", users[0].ID)})
	inst, _ := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	task := tasksOf(t, db, inst.ID)[0]
	if task.AssigneeID != users[1].ID {
		t.Fatalf("委托应换人, got %s want %s", task.AssigneeID, users[1].ID)
	}
}

func TestRejectEndsFlow(t *testing.T) {
	e, db := newTestEngine(t)
	users := seedUsers(t, db)
	n := userNode("n1", "审批", users[0].ID)
	n.OnReject = model.RejectEnd
	newFlow(t, db, []model.FlowNode{n, userNode("n2", "后续", users[1].ID)})
	inst, _ := e.Start(context.Background(), StartInput{FlowKey: "expense", InitiatorID: users[2].ID})
	task := tasksOf(t, db, inst.ID)[0]
	if err := e.HandleTask(context.Background(), task.ID, TaskInput{Action: model.TaskActReject, Comment: "不合规"}); err != nil {
		t.Fatal(err)
	}
	var got model.FlowInstance
	db.First(&got, inst.ID)
	if got.Status != model.FlowRejected {
		t.Fatalf("驳回应终止, got %s", got.Status)
	}
}
