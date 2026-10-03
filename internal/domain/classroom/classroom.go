package classroom

import (
	"errors"
	"strings"
)

var (
	ErrInvalidClassroomID  = errors.New("invalid classroom id")
	ErrMemberAlreadyExists = errors.New("classroom member already exists")
)

type ClassroomID string

func NewClassroomID(value string) (ClassroomID, error) {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 64 {
		return "", ErrInvalidClassroomID
	}
	return ClassroomID(value), nil
}

type MemberKind string

const (
	MemberTeacher MemberKind = "teacher"
	MemberStudent MemberKind = "student"
)

type Member struct {
	ID   string
	Kind MemberKind
}

type Classroom struct {
	ID      ClassroomID
	Name    string
	Members map[string]Member
}

func New(id ClassroomID, name string) *Classroom {
	return &Classroom{ID: id, Name: strings.TrimSpace(name), Members: make(map[string]Member)}
}

func (c *Classroom) AddMember(member Member) error {
	if member.ID == "" || (member.Kind != MemberTeacher && member.Kind != MemberStudent) {
		return errors.New("invalid classroom member")
	}
	if _, exists := c.Members[member.ID]; exists {
		return ErrMemberAlreadyExists
	}
	c.Members[member.ID] = member
	return nil
}

// CanControl keeps discovery and authorization separate: membership alone is not capability.
func (c *Classroom) CanControl(memberID string, capabilities map[string]bool) bool {
	_, memberExists := c.Members[memberID]
	return memberExists && capabilities["classroom.control"]
}
