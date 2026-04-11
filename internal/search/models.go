package search

type SearchRequest struct {
	Query     string    `json:"query" binding:"required"`
	Limit     int32     `json:"limit"`
	Offset    int32     `json:"offset"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchResult struct {
	ChunkID           int64    `json:"chunk_id"`
	DocumentID        int64    `json:"document_id"`
	BuildID           int64    `json:"build_id"`
	ChunkIndex        int32    `json:"chunk_index"`
	Content           string   `json:"content"`
	SectionPath       []string `json:"section_path"`
	SemanticType      *string  `json:"semantic_type,omitempty"`
	TokenCount        *int32   `json:"token_count,omitempty"`
	StartOffset       *int32   `json:"start_offset,omitempty"`
	EndOffset         *int32   `json:"end_offset,omitempty"`
	DocumentTitle     string   `json:"document_title"`
	SourceDisplayName *string  `json:"source_display_name,omitempty"`
	SourceLocator     *string  `json:"source_locator,omitempty"`
	SourceItemLocator *string  `json:"source_item_locator,omitempty"`
	LexicalScore      float64  `json:"lexical_score"`
	VectorScore       float64  `json:"vector_score"`
	CombinedScore     float64  `json:"combined_score"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type SearchDebugResponse struct {
	Lexical []SearchResult `json:"lexical"`
	Vector  []SearchResult `json:"vector"`
	Fused   []SearchResult `json:"fused"`
}
