package main

import (
	"context"
	"encoding/json"
	"fmt"

	"localmesh/internal/logging"
)

// GroupCastManager schedules per-group broadcast streams: each group gets a
// dedicated multicast port and its own content source (teacher screen or a
// demo student publishing directly).
type GroupCastManager struct{ c *Console }

// GroupStreamStatus describes one group's active stream.
type GroupStreamStatus struct {
	GroupID string `json:"group_id"`
	Port    int    `json:"port"`
	Source  string `json:"source"` // "teacher" | deviceID
	Active  bool   `json:"active"`
}

// groupPort derives the stable per-group multicast port.
func (g *GroupCastManager) groupPort(groupID string) int {
	return groupMulticastPort(groupID)
}

// groupAddr builds the multicast address for a group on the current channel.
func (g *GroupCastManager) groupAddr(groupID string) string {
	return fmt.Sprintf("239.255.42.1:%d", g.groupPort(groupID))
}

// Start begins a group stream with the given content source.
// source == "teacher" → the main capture loop fans out to the group sender.
// source == deviceID  → that student is commanded to publish directly to the
// group address (no teacher relay hop).
func (g *GroupCastManager) Start(ctx context.Context, groupID, source string) error {
	port := g.groupPort(groupID)
	addr := g.groupAddr(groupID)

	if source == "teacher" {
		// 主捕获循环扇出到组地址
		if err := g.c.startScreenStream("teacher"); err != nil && err.Error() != "screen stream already running" {
			return err
		}
		if err := g.c.fanOutToGroup(groupID, port); err != nil {
			return err
		}
	} else {
		// 学生直推：通知该学生发布到组地址
		payload, _ := json.Marshal(map[string]string{"addr": addr})
		ctl := &ControlService{c: g.c}
		ctl.queueCommand(source, "broadcast.demo_publish", string(payload))
	}

	// 组成员加入组地址
	join, _ := json.Marshal(map[string]string{"addr": addr})
	ctl := &ControlService{c: g.c}
	ctl.queueCommand("group:"+groupID, "broadcast.join_group", string(join))

	logging.Info("[GroupCast] group %s on air (source=%s port=%d)", groupID, source, port)
	return nil
}

// Stop ends one group's stream: fan-out removed, publisher stopped.
func (g *GroupCastManager) Stop(_ context.Context, groupID, source string) error {
	c := g.c
	if source != "" && source != "teacher" {
		stopPayload, _ := json.Marshal(map[string]any{"addr": g.groupAddr(groupID), "stop": true})
		ctl := &ControlService{c: c}
		ctl.queueCommand(source, "broadcast.demo_publish", string(stopPayload))
	}
	c.stopGroupFanOut(groupID)

	// 组成员退出组地址（回到频道主流地址）
	leave, _ := json.Marshal(map[string]string{"addr": classroomJoinAddr(c)})
	ctl := &ControlService{c: c}
	ctl.queueCommand("group:"+groupID, "broadcast.join_group", string(leave))

	logging.Info("[GroupCast] group %s off air", groupID)
	return nil
}

func classroomJoinAddr(c *Console) string {
	return c.currentScreenMulticast()
}

// Status lists every group's stream state.
func (g *GroupCastManager) Status(_ context.Context) []GroupStreamStatus {
	groups, members, err := g.c.store.ListGroups(context.Background())
	if err != nil {
		return []GroupStreamStatus{}
	}
	out := make([]GroupStreamStatus, 0, len(groups))
	for _, grp := range groups {
		out = append(out, GroupStreamStatus{
			GroupID: grp.GroupID,
			Port:    g.groupPort(grp.GroupID),
			Source:  "teacher",
			Active:  len(members[grp.GroupID]) > 0 && g.c.hasScreenStream(),
		})
	}
	return out
}

// hasScreenStream reports whether the main capture pipeline is running.
func (c *Console) hasScreenStream() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.screenStream != nil
}

// stopGroupFanOut removes one group's fan-out sender.
func (c *Console) stopGroupFanOut(groupID string) {
	c.mu.Lock()
	stream := c.screenStream
	c.mu.Unlock()
	if stream != nil {
		stream.stopOneGroup(groupID)
	}
}


