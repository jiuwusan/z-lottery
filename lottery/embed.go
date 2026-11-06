package assets

import (
	"embed"
)

// Files contains only the public assets, never the service or database files.
//
//go:embed index.html favicon.svg styles.css lottery-core.js app.js records.js
var Files embed.FS
