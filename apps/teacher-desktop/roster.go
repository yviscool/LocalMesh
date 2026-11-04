package main

import (
	"context"
	"errors"
	"time"

	"localmesh/internal/storage/sqlite"
)

// RosterService manages the student roster (花名册).
type RosterService struct{ c *Console }

// StudentView is a roster entry for the console UI.
type StudentView struct {
	StudentID string `json:"student_id"`
	Name      string `json:"name"`
	DeviceID  string `json:"device_id"`
}

// UpsertStudent adds or updates a roster entry.
func (s *RosterService) UpsertStudent(_ context.Context, studentID, name, deviceID string) error {
	if studentID == "" || name == "" {
		return errors.New("student id and name are required")
	}
	return s.c.store.UpsertStudent(context.Background(), sqlite.Student{
		StudentID: studentID, Name: name, DeviceID: deviceID, UpdatedAt: time.Now(),
	})
}

// RemoveStudent deletes a roster entry.
func (s *RosterService) RemoveStudent(_ context.Context, studentID string) error {
	return s.c.store.RemoveStudent(context.Background(), studentID)
}

// ListStudents returns the roster.
func (s *RosterService) ListStudents(_ context.Context) ([]StudentView, error) {
	students, err := s.c.store.ListStudents(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]StudentView, 0, len(students))
	for _, st := range students {
		out = append(out, StudentView{StudentID: st.StudentID, Name: st.Name, DeviceID: st.DeviceID})
	}
	return out, nil
}

// GroupService manages device groups (分组教学).
type GroupService struct{ c *Console }

// GroupView is a group with its members for the console UI.
type GroupView struct {
	GroupID string   `json:"group_id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// UpsertGroup adds or renames a group.
func (s *GroupService) UpsertGroup(_ context.Context, groupID, name string) error {
	if groupID == "" || name == "" {
		return errors.New("group id and name are required")
	}
	return s.c.store.UpsertGroup(context.Background(), sqlite.Group{GroupID: groupID, Name: name, UpdatedAt: time.Now()})
}

// DeleteGroup removes a group.
func (s *GroupService) DeleteGroup(_ context.Context, groupID string) error {
	return s.c.store.DeleteGroup(context.Background(), groupID)
}

// AddMember assigns a device to a group.
func (s *GroupService) AddMember(_ context.Context, groupID, deviceID string) error {
	return s.c.store.AddGroupMember(context.Background(), groupID, deviceID)
}

// RemoveMember unassigns a device from a group.
func (s *GroupService) RemoveMember(_ context.Context, groupID, deviceID string) error {
	return s.c.store.RemoveGroupMember(context.Background(), groupID, deviceID)
}

// ListGroups returns all groups with members.
func (s *GroupService) ListGroups(_ context.Context) ([]GroupView, error) {
	groups, members, err := s.c.store.ListGroups(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, GroupView{GroupID: g.GroupID, Name: g.Name, Members: members[g.GroupID]})
	}
	return out, nil
}

// GroupDevices expands a group target into its device ids at dispatch time.
func (c *Console) GroupDevices(groupID string) []string {
	_, members, err := c.store.ListGroups(context.Background())
	if err != nil {
		return nil
	}
	return members[groupID]
}
