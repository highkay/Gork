package storage

type LocalMediaCacheStatus struct {
	Media map[MediaType]LocalMediaCacheMediaStatus `json:"media"`
}

type LocalMediaCacheMediaStatus struct {
	Count           int   `json:"count"`
	Bytes           int64 `json:"bytes"`
	SaveCount       int   `json:"save_count"`
	ReconcileCount  int   `json:"reconcile_count"`
	EvictCount      int   `json:"evict_count"`
	LastReconcileAt int64 `json:"last_reconcile_at,omitempty"`
}

func (s *LocalMediaCacheStore) Status() (LocalMediaCacheStatus, error) {
	status := LocalMediaCacheStatus{
		Media: map[MediaType]LocalMediaCacheMediaStatus{
			MediaTypeImage: {},
			MediaTypeVideo: {},
		},
	}
	s.statsMu.Lock()
	for mediaType, item := range s.stats {
		status.Media[mediaType] = item
	}
	s.statsMu.Unlock()

	db, err := s.connectIndex()
	if err != nil {
		return status, err
	}
	defer db.Close()
	for _, mediaType := range []MediaType{MediaTypeImage, MediaTypeVideo} {
		item := status.Media[mediaType]
		item.Count, item.Bytes, err = mediaCacheUsage(db, mediaType)
		if err != nil {
			return status, err
		}
		status.Media[mediaType] = item
	}
	return status, nil
}
