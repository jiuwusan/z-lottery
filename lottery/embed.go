package assets

import (
	"embed"
)

// Files contains only the public assets, never the service or database files.
//
//go:embed index.html favicon.svg styles.css lottery-core.js app.js records.js install.js manifest.webmanifest icon-180.png icon-192.png icon-512.png
var Files embed.FS
