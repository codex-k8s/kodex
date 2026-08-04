package generated

type AnonymousSchema_24 struct {
	Slug        string              `json:"slug" binding:"required"`
	Description string              `json:"description" binding:"required"`
	Locale      *AnonymousSchema_27 `json:"locale" binding:"required"`
	Ownership   *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
