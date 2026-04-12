package search

import (
	"context"
	"sort"

	"github.com/pgvector/pgvector-go"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	lexical, vector, fused, err := s.search(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(req.Embedding) == 0 {
		return lexical, nil
	}
	if len(fused) == 0 {
		return vector, nil
	}
	return fused, nil
}

func (s *Service) Debug(ctx context.Context, req SearchRequest) (*SearchDebugResponse, error) {
	lexical, vector, fused, err := s.search(ctx, req)
	if err != nil {
		return nil, err
	}

	return &SearchDebugResponse{
		Lexical: lexical,
		Vector:  vector,
		Fused:   fused,
	}, nil
}

func (s *Service) search(ctx context.Context, req SearchRequest) ([]SearchResult, []SearchResult, []SearchResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	lexicalRows, err := s.db.Queries.SearchLexicalChunks(ctx, sqlc.SearchLexicalChunksParams{
		WebsearchToTsquery: req.Query,
		Limit:              limit,
		Offset:             req.Offset,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	lexical := make([]SearchResult, 0, len(lexicalRows))
	combined := make(map[int64]SearchResult, len(lexicalRows))
	for _, row := range lexicalRows {
		item := lexicalResult(row)
		lexical = append(lexical, item)
		combined[item.ChunkID] = item
	}

	vector := make([]SearchResult, 0)
	if len(req.Embedding) > 0 {
		vectorRows, err := s.db.Queries.SearchVectorChunks(ctx, sqlc.SearchVectorChunksParams{
			Embedding: pgvector.NewVector(req.Embedding),
			Limit:     limit,
			Offset:    req.Offset,
		})
		if err != nil {
			return nil, nil, nil, err
		}

		vector = make([]SearchResult, 0, len(vectorRows))
		for _, row := range vectorRows {
			item := vectorResult(row)
			vector = append(vector, item)
			if existing, ok := combined[item.ChunkID]; ok {
				existing.VectorScore = item.VectorScore
				existing.CombinedScore = existing.LexicalScore + existing.VectorScore
				combined[item.ChunkID] = existing
			} else {
				item.CombinedScore = item.VectorScore
				combined[item.ChunkID] = item
			}
		}
	}

	fused := make([]SearchResult, 0, len(combined))
	for _, item := range combined {
		if item.CombinedScore == 0 {
			item.CombinedScore = item.LexicalScore + item.VectorScore
		}
		fused = append(fused, item)
	}

	sort.Slice(lexical, func(i, j int) bool { return lexical[i].LexicalScore > lexical[j].LexicalScore })
	sort.Slice(vector, func(i, j int) bool { return vector[i].VectorScore > vector[j].VectorScore })
	sort.Slice(fused, func(i, j int) bool { return fused[i].CombinedScore > fused[j].CombinedScore })

	return lexical, vector, fused, nil
}

func lexicalResult(row sqlc.SearchLexicalChunksRow) SearchResult {
	return SearchResult{
		ChunkID:           row.ChunkID,
		DocumentID:        row.DocumentID,
		BuildID:           row.BuildID,
		ChunkIndex:        row.ChunkIndex,
		Content:           row.Content,
		SectionPath:       row.SectionPath,
		SemanticType:      apiutil.TextPtr(row.SemanticType),
		TokenCount:        apiutil.Int32Ptr(row.TokenCount),
		StartOffset:       apiutil.Int32Ptr(row.StartOffset),
		EndOffset:         apiutil.Int32Ptr(row.EndOffset),
		DocumentTitle:     row.DocumentTitle,
		SourceDisplayName: apiutil.TextPtr(row.SourceDisplayName),
		SourceLocator:     apiutil.TextPtr(row.SourceLocator),
		SourceItemLocator: apiutil.TextPtr(row.SourceItemLocator),
		LexicalScore:      row.LexicalScore,
		CombinedScore:     row.LexicalScore,
	}
}

func vectorResult(row sqlc.SearchVectorChunksRow) SearchResult {
	return SearchResult{
		ChunkID:           row.ChunkID,
		DocumentID:        row.DocumentID,
		BuildID:           row.BuildID,
		ChunkIndex:        row.ChunkIndex,
		Content:           row.Content,
		SectionPath:       row.SectionPath,
		SemanticType:      apiutil.TextPtr(row.SemanticType),
		TokenCount:        apiutil.Int32Ptr(row.TokenCount),
		StartOffset:       apiutil.Int32Ptr(row.StartOffset),
		EndOffset:         apiutil.Int32Ptr(row.EndOffset),
		DocumentTitle:     row.DocumentTitle,
		SourceDisplayName: apiutil.TextPtr(row.SourceDisplayName),
		SourceLocator:     apiutil.TextPtr(row.SourceLocator),
		SourceItemLocator: apiutil.TextPtr(row.SourceItemLocator),
		VectorScore:       row.VectorScore,
		CombinedScore:     row.VectorScore,
	}
}
