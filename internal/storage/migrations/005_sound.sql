ALTER TABLE sessions ADD COLUMN volume_percent INTEGER CHECK (volume_percent IS NULL OR volume_percent BETWEEN 0 AND 100);
ALTER TABLE sessions ADD COLUMN volume_muted INTEGER CHECK (volume_muted IS NULL OR volume_muted IN (0, 1));
