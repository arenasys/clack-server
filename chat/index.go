package chat

import (
	. "clack/common"
	"clack/storage"
	"cmp"
	"math"
	"slices"
	"sort"
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
	Users     map[Snowflake]User
	UserInfos map[Snowflake]*UserInfo
	Roles     map[Snowflake]Role
	Channels  map[Snowflake]Channel
	List      UserList
	Settings  Settings

	Stale   bool
	Mutex   sync.RWMutex
	Changes []IndexRange
}

// Populates the index from the database (settings, roles, channels, users)
func (i *Index) PopulateIndex(conn *sqlite.Conn) {
	startTotal := time.Now()
	i.Mutex.Lock()
	tx := storage.NewTransaction(conn)
	tx.Start()

	i.Users = make(map[Snowflake]User)
	i.Roles = make(map[Snowflake]Role)
	i.Channels = make(map[Snowflake]Channel)
	i.UserInfos = make(map[Snowflake]*UserInfo)

	startSettings := time.Now()
	settings, err := tx.GetSettings()
	if err != nil {
		panic(err)
	}
	i.Settings = settings
	gwLog.Printf("Index.PopulateIndex: settings loaded in %v", time.Since(startSettings))

	startRoles := time.Now()
	roles, err := tx.GetAllRoles()
	if err != nil {
		panic(err)
	}
	for _, r := range roles {
		i.Roles[r.ID] = r
	}
	gwLog.Printf("Index.PopulateIndex: roles loaded in %v", time.Since(startRoles))

	startChannels := time.Now()
	channels, err := tx.GetAllChannels()
	if err != nil {
		panic(err)
	}
	for _, c := range channels {
		i.Channels[c.ID] = c
	}
	gwLog.Printf("Index.PopulateIndex: channels loaded in %v", time.Since(startChannels))

	startUsers := time.Now()
	users, err := tx.GetAllUsers()
	if err != nil {
		panic(err)
	}
	for _, u := range users {
		i.Users[u.ID] = i.processUser(u)
		i.UserInfos[u.ID] = &UserInfo{}
	}
	gwLog.Printf("Index.PopulateIndex: users loaded in %v", time.Since(startUsers))

	tx.Commit(nil)
	i.Mutex.Unlock()
	gwLog.Printf("Index.PopulateIndex: total time %v", time.Since(startTotal))
}

func (i *Index) UpdateUserInfos() {
	start := time.Now()

	i.Mutex.RLock()

	arrayStart := time.Now()
	ids := make([]Snowflake, 0, len(i.UserInfos))
	for id := range i.UserInfos {
		ids = append(ids, id)
	}
	gwLog.Printf("Index.ComputeUserInfos: collected %d IDs in %v", len(ids), time.Since(arrayStart))

	totalIDs := len(ids)

	workerCount := 8
	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for idx := worker; idx < totalIDs; idx += workerCount {
				id := ids[idx]
				user, ok := i.Users[id]
				if !ok {
					continue
				}
				*i.UserInfos[id] = i.computeUserInfo(user)
			}
		}(w)
	}

	wg.Wait()
	i.Mutex.RUnlock()

	totalTime := time.Since(start)
	gwLog.Printf("Index.ComputeUserInfos: total %v (workers=%d, ids=%d)", totalTime, workerCount, totalIDs)
}

// Sorts and groups users for the user list
func (i *Index) SortUserList() {
	start := time.Now()
	i.UpdateUserList()
	gwLog.Printf("Index.SortUserList: user list updated in %v", time.Since(start))
}

func (i *Index) getUserGroup(u *User, ui *UserInfo) Snowflake {
	if ui.Hoist != 0 {
		return ui.Hoist
	}
	if u.IsOnline() {
		return Snowflake(UserPresenceOnline)
	}
	return Snowflake(UserPresenceOffline)
}

func (i *Index) UpdateUserList() {
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
		group := i.getUserGroup(&u, ui)
		next.Groups[group] = append(next.Groups[group], uid)
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

	var changes []IndexRange = []IndexRange{}
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
	i.Stale = false
	i.Changes = append(i.Changes, changes...)
	i.Mutex.Unlock()
}

func (i *Index) RepositionUser(id Snowflake) {
	start := time.Now()

	user, ok := i.Users[id]
	if !ok {
		return
	}

	ui := i.UserInfos[id]

	// remove from old group
	var old_group Snowflake
	var old_group_pos int = -1
	for gid, arr := range i.List.Groups {
		for j, uid := range arr {
			if uid == id {
				old_group = gid
				old_group_pos = j
				break
			}
		}
		if old_group_pos != -1 {
			break
		}
	}
	if old_group_pos == -1 {
		return
	}
	old_group_users := i.List.Groups[old_group]
	old_group_users = append(old_group_users[:old_group_pos], old_group_users[old_group_pos+1:]...)
	i.List.Groups[old_group] = old_group_users

	// insert into new group
	new_group := i.getUserGroup(&user, ui)
	new_group_users := i.List.Groups[new_group]
	new_group_pos := sort.Search(len(new_group_users), func(j int) bool {
		other := i.Users[new_group_users[j]]
		if c := strings.Compare(user.DisplayName, other.DisplayName); c != 0 {
			return c < 0
		}
		return id < new_group_users[j]
	})
	new_group_users = append(new_group_users, 0)
	copy(new_group_users[new_group_pos+1:], new_group_users[new_group_pos:])
	new_group_users[new_group_pos] = id
	i.List.Groups[new_group] = new_group_users

	// find old view position
	old_view_pos := -1
	for idx, vid := range i.List.View {
		if vid == id {
			old_view_pos = idx
			break
		}
	}

	// rebuild view
	new_view := make([]Snowflake, 0, len(i.Users)+len(i.List.GroupOrder))
	for _, gid := range i.List.GroupOrder {
		new_view = append(new_view, gid)
		new_view = append(new_view, i.List.Groups[gid]...)
	}
	i.List.View = new_view

	// find new view position
	new_view_pos := -1
	for idx, vid := range i.List.View {
		if vid == id {
			new_view_pos = idx
			break
		}
	}
	if new_view_pos == -1 {
		gwLog.Printf("RepositionUser: user %v not in new view", id)
		return
	}
	from := old_view_pos
	to := new_view_pos
	if from > to {
		from, to = to, from
	}
	change := IndexRange{
		From: from,
		To:   to + 1,
	}
	i.Changes = append(i.Changes, change)

	gwLog.Printf("RepositionUser: took %v", time.Since(start))
}

func (i *Index) PopAllChanges() []IndexRange {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	changes := i.Changes
	i.Changes = nil
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
	perms := i.Settings.DefaultPermissions

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
	if (perms & PermissionAdministrator) != 0 {
		perms = PermissionAll
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
	start := time.Now()
	i.PopulateIndex(conn)
	i.UpdateUserInfos()
	i.SortUserList()
	gwLog.Printf("Index.Build: total time %v", time.Since(start))
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
	users := []User{}
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
	i.UserInfos[user.ID] = &UserInfo{}
	*i.UserInfos[user.ID] = i.computeUserInfo(user)
	i.Stale = true
	return i.Users[user.ID]
}

func (i *Index) UpdateUser(user User) User {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Users[user.ID] = i.processUser(user)
	if i.UserInfos[user.ID] == nil {
		i.UserInfos[user.ID] = &UserInfo{}
	}
	*i.UserInfos[user.ID] = i.computeUserInfo(user)

	i.RepositionUser(user.ID)

	return i.Users[user.ID]
}

func (i *Index) DeleteUser(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Users, id)
	delete(i.UserInfos, id)
	i.Stale = true
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
	roles := []Role{}
	for _, role := range i.Roles {
		roles = append(roles, role)
	}
	return roles
}

func (i *Index) AddRole(role Role) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Roles[role.ID] = role
	i.Stale = true
}

func (i *Index) UpdateRole(role Role) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Roles[role.ID] = role
	i.Stale = true
}

func (i *Index) DeleteRole(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Roles, id)
	i.Stale = true
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
	channels := []Channel{}
	for _, channel := range i.Channels {
		channels = append(channels, channel)
	}
	return channels
}

func (i *Index) AddChannel(channel Channel) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Channels[channel.ID] = channel
	i.Stale = true
}

func (i *Index) UpdateChannel(channel Channel) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()

	i.Channels[channel.ID] = channel
	i.Stale = true
}

func (i *Index) DeleteChannel(id Snowflake) {
	i.Mutex.Lock()
	defer i.Mutex.Unlock()
	delete(i.Channels, id)
	i.Stale = true
}

func (i *Index) GetPermissionsByUser(userID Snowflake) int {
	info, ok := i.UserInfos[userID]
	if !ok || info == nil {
		return 0
	}
	return info.Permissions
}

func (i *Index) GetPermissionsByChannel(userID Snowflake, channelID Snowflake) int {
	var allow int = i.GetPermissionsByUser(userID)
	var deny int = 0
	var user User
	var ok bool

	if user, ok = i.GetUser(userID); !ok {
		return 0
	}

	if channelID != 0 {
		channel, ok := i.GetChannel(channelID)
		if ok {
			for _, overwrite := range channel.Overwrites {
				if overwrite.Type == OverwriteTypeRole {
					for _, roleID := range user.Roles {
						if overwrite.ID == roleID {
							allow |= overwrite.Allow
							deny |= overwrite.Deny
							break
						}
					}
				} else if overwrite.Type == OverwriteTypeUser {
					if overwrite.ID == user.ID {
						allow |= overwrite.Allow
						deny |= overwrite.Deny
					}
				}
			}
		}
	}

	permissions := allow & (^deny)

	if (permissions & PermissionAdministrator) != 0 {
		return PermissionAll
	}

	return permissions
}
