-- +goose Up
CREATE TABLE IF NOT EXISTS screenshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL UNIQUE,
    data BLOB NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'image/webp',
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_screenshots_url ON screenshots(url);

-- +goose Down
DROP TABLE IF EXISTS screenshots;
