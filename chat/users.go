package chat

import (
	. "clack/common"
	"clack/storage"
	"slices"
	"strings"
	"sync"

	"zombiezen.com/go/sqlite"
)

var (
	userIndex *UserIndex
)

func GetUserIndex(conn *sqlite.Conn) *UserIndex {
	if userIndex == nil {
		userIndex = &UserIndex{
			Users: map[Snowflake]User{},
			Roles: map[Snowflake]Role{},
		}
		userIndex.Populate(conn)
	}
	return userIndex
}

type UserIndex struct {
	Users  map[Snowflake]User
	Roles  map[Snowflake]Role
	Groups []UserListGroup
	Mutex  sync.Mutex
}

type UserListGroup struct {
	ID    Snowflake   `json:"id"`
	Count int         `json:"count"`
	Start int         `json:"start"`
	Users []Snowflake `json:"users"`
}

func (i *UserIndex) Populate(conn *sqlite.Conn) {
	tx := storage.NewTransaction(conn)
	tx.Start()
	users := tx.GetAllUsers()
	roles := tx.GetAllRoles()
	tx.Commit(nil)

	for _, role := range roles {
		i.Roles[role.ID] = role
	}

	for _, user := range users {
		i.Users[user.ID] = user
	}
}

func (i *UserIndex) GetGroups() []UserListGroup {
	if i.Groups != nil {
		return i.Groups
	}

	groups := map[Snowflake][]Snowflake{}
	groupOrder := []Snowflake{}

	hoistableRoles := []Role{}
	for _, role := range i.Roles {
		if role.Hoisted {
			hoistableRoles = append(hoistableRoles, role)
		}
	}
	slices.SortFunc(hoistableRoles, func(a, b Role) int {
		return a.Position - b.Position
	})

	for _, role := range hoistableRoles {
		groups[role.ID] = []Snowflake{}
		groupOrder = append(groupOrder, role.ID)
	}

	groupOrder = append(groupOrder, UserStatusOnline)
	groups[UserStatusOnline] = []Snowflake{}

	if len(i.Users) <= 1000 {
		groupOrder = append(groupOrder, UserStatusOffline)
		groups[UserStatusOffline] = []Snowflake{}
	}

	var userGroupPosition int
	var userGroup Snowflake
	for _, user := range i.Users {
		userGroupPosition = 1 << 16
		userGroup = UserStatusOffline

		if user.IsOnline() {
			userGroup = UserStatusOnline
		}

		for _, roleId := range user.Roles {
			if role, ok := i.Roles[roleId]; ok {
				if role.Hoisted && role.Position < userGroupPosition {
					userGroup = role.ID
					userGroupPosition = role.Position
				}
			}
		}

		if groups[userGroup] != nil {
			groups[userGroup] = append(groups[userGroup], user.ID)
		}
	}

	var response []UserListGroup
	for _, groupID := range groupOrder {
		if users, ok := groups[groupID]; ok {
			slices.SortFunc(users, func(a, b Snowflake) int {
				return strings.Compare(i.Users[a].DisplayName(), i.Users[b].DisplayName())
			})

			/*slices.SortFunc(users, func(a, b Snowflake) int {
				return int(a - b)
			})*/

			response = append(response, UserListGroup{
				ID:    groupID,
				Count: len(users),
				Start: 0,
				Users: users,
			})
		}
	}

	i.Mutex.Lock()
	i.Groups = response
	i.Mutex.Unlock()

	return response
}

func (i *UserIndex) GetUser(id Snowflake) (User, bool) {
	user, ok := i.Users[id]
	return user, ok
}

func (i *UserIndex) GetRole(id Snowflake) (Role, bool) {
	role, ok := i.Roles[id]
	return role, ok
}

func (i *UserIndex) GetAllRoles() []Role {
	var roles []Role
	for _, role := range i.Roles {
		roles = append(roles, role)
	}
	return roles
}

func (i *UserIndex) GetUsers(ids []Snowflake) []User {
	var users []User
	for _, id := range ids {
		if user, ok := i.Users[id]; ok {
			users = append(users, user)
		}
	}
	return users
}

func (i *UserIndex) GetUserListSlice(req UserListRequest, limit int) UserListResponse {
	var response UserListResponse = UserListResponse{
		StartGroup: req.StartGroup,
		StartIndex: req.StartIndex,
		EndGroup:   req.EndGroup,
		EndIndex:   req.EndIndex,
		Groups:     []UserListGroup{},
	}

	groups := i.GetGroups()
	count := -1

	for _, group := range groups {
		responseGroup := UserListGroup{
			ID:    group.ID,
			Count: group.Count,
			Start: 0,
			Users: []Snowflake{},
		}

		if count == -1 && (req.StartGroup == group.ID || req.StartGroup == -1) {
			count = 0
			responseGroup.Start = req.StartIndex
		}

		if count >= 0 {
			for i, user := range group.Users {
				if i < responseGroup.Start || count >= limit {
					continue
				}

				responseGroup.Users = append(responseGroup.Users, user)
				count++

				if req.EndGroup == group.ID && req.EndIndex == i {
					break
				}
			}
		}

		response.Groups = append(response.Groups, responseGroup)
	}

	return response
}

func (i *UserIndex) AddUser(user User) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	if _, exists := i.Users[user.ID]; exists {
		return
	}

	i.Users[user.ID] = user

	// todo: update instead of rebuild
	i.Groups = nil
}
