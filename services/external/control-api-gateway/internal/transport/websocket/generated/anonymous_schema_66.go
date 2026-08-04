package generated

type AnonymousSchema_66 struct {
	Revision      int                 `json:"revision" binding:"required"`
	ContentSha256 string              `json:"contentSha256" binding:"required"`
	SourceRef     string              `json:"sourceRef" binding:"required"`
	Locale        string              `json:"locale" binding:"required"`
	Ownership     *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
