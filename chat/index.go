package chat

import (
	. "clack/common"
	"clack/storage"
	"cmp"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"zombiezen.com/go/sqlite"
)

type UserInfo struct {
	Rank        int
	Hoist       Snowflake
	Permissions int
}

type SortEntry struct {
	ID  Snowflake
	Key string
}

type IndexRange struct {
	From int // inclusive
	To   int // exclusive
}

func (a IndexRange) Overlaps(b IndexRange) bool {
	if a.To <= a.From || b.To <= b.From {
		return false
	}
	return a.From < b.To && b.From < a.To
}

type UserList struct {
	GroupOrder []Snowflake
	Groups     map[Snowflake][]Snowflake
	View       []Snowflake
}

type Index struct {
	Users       map[Snowflake]User
	UserInfos   map[Snowflake]UserInfo
	Roles       map[Snowflake]Role
	Channels    map[Snowflake]Channel
	List        UserList
	Invalidated bool
	Mutex       sync.RWMutex
}

func (i *Index) UpdateUserList() []IndexRange {
	i.Mutex.RLock()

	next := UserList{}
	next.GroupOrder = []Snowflake{}

	for _, r := range i.Roles {
		if r.Hoisted {
			next.GroupOrder = append(next.GroupOrder, r.ID)
		}
	}
	slices.SortFunc(next.GroupOrder, func(a, b Snowflake) int {
		return cmp.Compare(i.Roles[a].Position, i.Roles[b].Position)
	})

	onlineID := Snowflake(UserPresenceOnline)
	offlineID := Snowflake(UserPresenceOffline)
	next.GroupOrder = append(next.GroupOrder, onlineID, offlineID)

	next.Groups = make(map[Snowflake][]Snowflake)
	for _, gid := range next.GroupOrder {
		next.Groups[gid] = []Snowflake{}
	}

	for uid, u := range i.Users {
		ui := i.UserInfos[uid]
		if ui.Hoist != 0 {
			next.Groups[ui.Hoist] = append(next.Groups[ui.Hoist], uid)
		} else {
			if u.IsOnline() {
				next.Groups[onlineID] = append(next.Groups[onlineID], uid)
			} else {
				next.Groups[offlineID] = append(next.Groups[offlineID], uid)
			}
		}
	}

	start_sort := time.Now()

	for _, groupID := range next.GroupOrder {
		entries := make([]SortEntry, 0, len(next.Groups[groupID]))
		for _, uid := range next.Groups[groupID] {
			entries = append(entries, SortEntry{
				ID:  uid,
				Key: i.Users[uid].DisplayName,
			})
		}
		slices.SortFunc(entries, func(a, b SortEntry) int {
			if c := strings.Compare(a.Key, b.Key); c != 0 {
				return c
			}
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
		for i, e := range entries {
			next.Groups[groupID][i] = e.ID
		}
	}

	gwLog.Printf("Sorted user list in %v", time.Since(start_sort))

	next.View = make([]Snowflake, 0, len(i.Users)+len(next.GroupOrder))
	for _, gid := range next.GroupOrder {
		next.View = append(next.View, gid)
		next.View = append(next.View, next.Groups[gid]...)
	}

	var changes []IndexRange
	var start int = -1

	for idx := 0; idx < len(i.List.View); idx++ {
		changed := false
		if idx >= len(next.View) {
			changed = true
		} else if next.View[idx] != i.List.View[idx] {
			changed = true
		}

		if changed {
			if start == -1 {
				start = idx
			}
		} else {
			if start != -1 {
				changes = append(changes, IndexRange{
					From: start,
					To:   idx,
				})
				start = -1
			}
		}
	}

	if start != -1 {
		changes = append(changes, IndexRange{
			From: start,
			To:   len(i.List.View),
		})
	} else if len(next.View) > len(i.List.View) {
		changes = append(changes, IndexRange{
			From: len(i.List.View),
			To:   len(i.List.View),
		})
	}
	i.Mutex.RUnlock()

	i.Mutex.Lock()
	i.List = next
	i.Invalidated = false
	i.Mutex.Unlock()

	return changes
}

func (i *Index) GetUserListSlice(start, end, limit int) UserListResponse {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()

	total := len(i.List.View)
	start = ClampInt(start, 0, total)
	end = ClampInt(end, start, total)

	if limit > 0 && end-start > limit {
		end = start + limit
	}

	resp := UserListResponse{
		Start: start,
		End:   end,
		Slice: append([]Snowflake(nil), i.List.View[start:end]...),
	}

	resp.Groups = make([]UserListGroup, 0, len(i.List.GroupOrder))
	for _, gid := range i.List.GroupOrder {
		resp.Groups = append(resp.Groups, UserListGroup{
			ID:    gid,
			Count: len(i.List.Groups[gid]),
		})
	}

	return resp
}

func (i *Index) computeUserInfo(u User) UserInfo {
	rank := math.MaxInt
	hoistRank := math.MaxInt
	hoist := Snowflake(0)
	perms := 0

	for _, roleID := range u.Roles {
		role, ok := i.Roles[roleID]
		if !ok {
			continue
		}
		if role.Position < rank { // lower position = better rank
			rank = role.Position
		}
		if role.Hoisted && role.Position < hoistRank {
			hoistRank = role.Position
			hoist = role.ID
		}
		perms |= role.Permissions
	}

	return UserInfo{
		Rank:        rank,
		Hoist:       hoist,
		Permissions: perms,
	}
}

func (i *Index) processUser(user User) User {
	user.Presence = UserPresenceOffline
	return user
}

func (i *Index) Build(conn *sqlite.Conn) {
	i.Mutex.Lock()
	tx := storage.NewTransaction(conn)
	tx.Start()

	i.Users = make(map[Snowflake]User)
	i.Roles = make(map[Snowflake]Role)
	i.Channels = make(map[Snowflake]Channel)
	i.UserInfos = make(map[Snowflake]UserInfo)

	roles, err := tx.GetAllRoles()
	if err != nil {
		panic(err)
	}
	for _, r := range roles {
		i.Roles[r.ID] = r
	}

	channels, err := tx.GetAllChannels()
	if err != nil {
		panic(err)
	}
	for _, c := range channels {
		i.Channels[c.ID] = c
	}

	users, err := tx.GetAllUsers()
	if err != nil {
		panic(err)
	}
	for _, u := range users {
		i.Users[u.ID] = i.processUser(u)
		i.UserInfos[u.ID] = i.computeUserInfo(u)
	}
	tx.Commit(nil)
	i.Mutex.Unlock()

	i.UpdateUserList()
}

func (i *Index) GetUser(id Snowflake) (User, bool) {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	user, ok := i.Users[id]
	return user, ok
}

func (i *Index) GetUsers(ids []Snowflake) []User {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	var users []User
	for _, id := range ids {
		if user, ok := i.Users[id]; ok {
			users = append(users, user)
		}
	}
	return users
}

func (i *Index) AddUser(user User) User {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Users[user.ID] = i.processUser(user)
	i.UserInfos[user.ID] = i.computeUserInfo(user)
	i.Invalidated = true
	return i.Users[user.ID]
}

func (i *Index) UpdateUser(user User) User {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Users[user.ID] = i.processUser(user)
	i.UserInfos[user.ID] = i.computeUserInfo(user)
	i.Invalidated = true
	return i.Users[user.ID]
}

func (i *Index) DeleteUser(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Users, id)
	delete(i.UserInfos, id)
	i.Invalidated = true
}

func (i *Index) GetRole(id Snowflake) (Role, bool) {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	role, ok := i.Roles[id]
	return role, ok
}

func (i *Index) GetAllRoles() []Role {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	var roles []Role
	for _, role := range i.Roles {
		roles = append(roles, role)
	}
	return roles
}

func (i *Index) AddRole(role Role) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Roles[role.ID] = role
	i.Invalidated = true
}

func (i *Index) UpdateRole(role Role) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Roles[role.ID] = role
	i.Invalidated = true
}

func (i *Index) DeleteRole(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Roles, id)
	i.Invalidated = true
}

func (i *Index) GetChannel(id Snowflake) (Channel, bool) {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	channel, ok := i.Channels[id]
	return channel, ok
}

func (i *Index) GetAllChannels() []Channel {
	i.Mutex.RLock()
	defer i.Mutex.RUnlock()
	var channels []Channel
	for _, channel := range i.Channels {
		channels = append(channels, channel)
	}
	return channels
}

func (i *Index) AddChannel(channel Channel) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Channels[channel.ID] = channel
	i.Invalidated = true
}

func (i *Index) UpdateChannel(channel Channel) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Channels[channel.ID] = channel
	i.Invalidated = true
}

func (i *Index) DeleteChannel(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Channels, id)
	i.Invalidated = true
}

func (i *Index) Invalidate() {
	i.Invalidated = true
}
