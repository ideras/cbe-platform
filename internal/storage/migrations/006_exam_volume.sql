ALTER TABLE exams ADD COLUMN min_volume_percent INTEGER NOT NULL DEFAULT 80 CHECK (min_volume_percent BETWEEN 0 AND 100);
