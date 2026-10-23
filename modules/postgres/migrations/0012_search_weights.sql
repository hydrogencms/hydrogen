-- Search text has three relevance tiers: setweight 'A' (high), 'B' (normal) and 'C' (low), ranked by ts_rank's
-- default weights. Existing rows were built unweighted ('D'); they become the normal tier, which is what an
-- unweighted schema indexes.
UPDATE content_search SET tsv = setweight(tsv, 'B');
