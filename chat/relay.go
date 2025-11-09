package chat

func (gw *Gateway) Relay(event Event) {
	go func() {
		for _, conn := range gw.connections {
			conn.Relay(&event)
		}
	}()
}

func (gw *Gateway) OnMessageAdd(msg *MessageAddEvent) {
	event := Event{
		Type: EventTypeMessageAdd,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnMessageDelete(msg *MessageDeleteEvent) {
	event := Event{
		Type: EventTypeMessageDelete,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnMessageUpdate(msg *MessageUpdateEvent) {
	event := Event{
		Type: EventTypeMessageUpdate,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnReactionAdd(msg *ReactionAddEvent) {
	event := Event{
		Type: EventTypeMessageReactionAdd,
		Data: msg,
	}

	gw.Relay(event)
}

func (gw *Gateway) OnReactionDelete(msg *ReactionDeleteEvent) {
	event := Event{
		Type: EventTypeMessageReactionDelete,
		Data: msg,
	}

	gw.Relay(event)
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
