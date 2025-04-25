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

func (gw *Gateway) OnMessageUpdate(msg *MessageUpdateEvent) {
	event := Event{
		Type: EventTypeMessageUpdate,
		Data: msg,
	}

	gw.Relay(event)
}
