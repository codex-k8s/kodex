package generated

type Problem struct {
	ReservedType string `json:"type" binding:"required"`
	RequestId    string `json:"requestId" binding:"required"`
	Code         string `json:"code" binding:"required"`
	Retryable    bool   `json:"retryable" binding:"required"`
}
