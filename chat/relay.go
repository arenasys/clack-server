package chat

import . "clack/common"

func (gw *Gateway) Relay(event Event) {
	go func() {
		gw.connectionsMutex.RLock()
		defer gw.connectionsMutex.RUnlock()
		for _, conn := range gw.connections {
			conn.Relay(&event)
		}
	}()
}

func (gw *Gateway) RelayByChannel(event Event, channelID Snowflake) {
	go func() {
		index := gw.GetIndex()

		gw.connectionsMutex.RLock()
		defer gw.connectionsMutex.RUnlock()

		for _, conn := range gw.connections {
			if !conn.Authenticated() {
				continue
			}

			perms := index.GetPermissionsByChannel(conn.userID, channelID)
			if perms&PermissionViewChannel == 0 {
				continue
			}

			conn.Relay(&event)
		}
	}()
}

func (gw *Gateway) OnMessageAdd(msg *MessageAddEvent) {
	event := Event{
		Type: EventTypeMessageAdd,
		Data: msg,
	}

	gw.RelayByChannel(event, msg.Message.ChannelID)
}

func (gw *Gateway) OnMessageDelete(msg *MessageDeleteEvent, channelID Snowflake) {
	event := Event{
		Type: EventTypeMessageDelete,
		Data: msg,
	}

	gw.RelayByChannel(event, channelID)
}

func (gw *Gateway) OnMessageUpdate(msg *MessageUpdateEvent) {
	event := Event{
		Type: EventTypeMessageUpdate,
		Data: msg,
	}

	gw.RelayByChannel(event, msg.Message.ChannelID)
}

func (gw *Gateway) OnReactionAdd(msg *ReactionAddEvent, channelID Snowflake) {
	event := Event{
		Type: EventTypeMessageReactionAdd,
		Data: msg,
	}

	gw.RelayByChannel(event, channelID)
}

func (gw *Gateway) OnReactionDelete(msg *ReactionDeleteEvent, channelID Snowflake) {
	event := Event{
		Type: EventTypeMessageReactionDelete,
		Data: msg,
	}

	gw.RelayByChannel(event, channelID)
}

func (gw *Gateway) OnUserAdd(msg *UserAddEvent) {
	event := Event{
		Type: EventTypeUserAdd,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnUserDelete(msg *UserDeleteEvent) {
	event := Event{
		Type: EventTypeUserDelete,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnUserUpdate(msg *UserUpdateEvent) {
	event := Event{
		Type: EventTypeUserUpdate,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnRoleAdd(msg *RoleAddEvent) {
	event := Event{
		Type: EventTypeRoleAdd,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnRoleDelete(msg *RoleDeleteEvent) {
	event := Event{
		Type: EventTypeRoleDelete,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnRoleUpdate(msg *RoleUpdateEvent) {
	event := Event{
		Type: EventTypeRoleUpdate,
		Data: msg,
	}

	gw.Relay(event)
}
