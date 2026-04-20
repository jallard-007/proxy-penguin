package api

/*

	query := r.URL.Query()
	connID := query.Get("cid")
	if connID != "" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		s.registerStream(connID, cancel)
		defer s.unregisterStream(connID)
	}

	var lastId int64
	var lastIdOk bool
	_ = lastId
	_ = lastIdOk

	if vals, ok := r.Header["Last-Event-Id"]; ok && len(vals) > 0 {
		lastIdStr := vals[0]
		id, err := strconv.ParseInt(lastIdStr, 10, 64)
		if err == nil {
			lastId = id
			lastIdOk = true
		}
	}

*/
