package storage

type LocalMediaCacheStatus struct {
	Media map[MediaType]LocalMediaCacheMediaStatus `json:"media"`
}

type LocalMediaCacheMediaStatus struct {
	Count               int                        `json:"count"`
	Bytes               int64                      `json:"bytes"`
	LimitBytes          int                        `json:"limit_bytes"`
	EvictionPolicy      string                     `json:"eviction_policy"`
	SaveCount           int                        `json:"save_count"`
	ReconcileCount      int                        `json:"reconcile_count"`
	EvictCount          int                        `json:"evict_count"`
	LastReconcileAt     int64                      `json:"last_reconcile_at,omitempty"`
	LastReconcileReport *MediaCacheReconcileReport `json:"last_reconcile_report,omitempty"`
}

type MediaCacheReconcileReport struct {
	OrphanIndexRows int      `json:"orphan_index_rows"`
	OrphanFiles     int      `json:"orphan_files"`
	TempFiles       int      `json:"temp_files"`
	RemovedBytes    int64    `json:"removed_bytes"`
	RemovedNames    []string `json:"removed_names,omitempty"`
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
		item.LimitBytes = s.limitBytes(mediaType)
		if item.LimitBytes > 0 {
			item.EvictionPolicy = "lru_updated_at_low_watermark_60"
		} else {
			item.EvictionPolicy = "none_limit_zero"
		}
		item.Count, item.Bytes, err = mediaCacheUsage(db, mediaType)
		if err != nil {
			return status, err
		}
		status.Media[mediaType] = item
	}
	return status, nil
}
