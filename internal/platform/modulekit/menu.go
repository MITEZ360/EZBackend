package modulekit

import "sort"

// MenuNode 是组装后的菜单树节点。
type MenuNode struct {
	Menu
	Children []MenuNode
}

// 内置导航分组。模块声明页面时把 Parent 指向这里，无需各自重复定义目录。
// 目录本身也要由某个模块声明（见各模块 Menus 的第一项），全部相关模块被
// 裁掉后目录不会出现，侧边栏不留空分组。
var (
	DirSystem = Menu{ID: "m_system", Name: "系统管理", Type: "dir", Path: "/system", Icon: "settings", Sort: 10}
	DirGov    = Menu{ID: "m_gov", Name: "服务治理", Type: "dir", Path: "/gov", Icon: "share", Sort: 15}
	DirOps    = Menu{ID: "m_ops", Name: "运维中心", Type: "dir", Path: "/ops", Icon: "server", Sort: 20}
)

// Menus 按启用集合组装菜单树。
//
// 规则：
//   - 未启用模块声明的菜单整体丢弃（连带其子节点）；
//   - 同 ID 的目录允许多个模块重复声明，只保留最先注册的一份，
//     避免共享目录被反复覆盖；
//   - 目录的子项全部被裁掉后目录自身也消失，侧边栏不留空分组。
//
// enabled 为 nil 或空表示全部启用。
func Menus(enabled map[string]bool) []MenuNode {
	var flat []seqMenu
	seen := map[string]bool{}
	seq := 0
	for _, s := range All() {
		if len(enabled) > 0 && !enabled[s.Name] {
			continue
		}
		for _, m := range s.Menus {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			flat = append(flat, seqMenu{Menu: m, seq: seq})
			seq++
		}
	}
	return buildTree(flat, "")
}

type seqMenu struct {
	Menu
	seq int
}

// buildTree 把扁平声明按 Parent 组装成树。同级节点按 Sort 升序，
// Sort 相同时保持声明顺序（按钮普遍不写 Sort）。
func buildTree(items []seqMenu, parent string) []MenuNode {
	var level []seqMenu
	for _, it := range items {
		if it.Parent == parent {
			level = append(level, it)
		}
	}
	sort.SliceStable(level, func(i, j int) bool { return level[i].Sort < level[j].Sort })

	out := make([]MenuNode, 0, len(level))
	for _, it := range level {
		children := buildTree(items, it.ID)
		// 目录本身没有页面与权限点，子项全部被裁掉后留着只是空壳。
		// 只在"声明阶段确实有子项"时才隐藏，避免误伤手动挂着页面的空目录。
		if it.Type == "dir" && len(children) == 0 && hasChildDecl(items, it.ID) {
			continue
		}
		out = append(out, MenuNode{Menu: it.Menu, Children: children})
	}
	return out
}

// hasChildDecl 判断某个目录在声明阶段是否本应有子节点。
func hasChildDecl(items []seqMenu, id string) bool {
	for _, it := range items {
		if it.Parent == id {
			return true
		}
	}
	return false
}

// WalkMenus 深度优先遍历菜单树。
func WalkMenus(nodes []MenuNode, fn func(Menu)) {
	for _, n := range nodes {
		fn(n.Menu)
		WalkMenus(n.Children, fn)
	}
}
