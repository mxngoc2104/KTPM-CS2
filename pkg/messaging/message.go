package messaging

// JobMessage represents the data sent over Kafka for a processing job.
type JobMessage struct {
	JobID     string `json:"job_id"`
	ImagePath string `json:"image_path"`
}
