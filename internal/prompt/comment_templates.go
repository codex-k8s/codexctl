package prompt

import "embed"

//go:embed templates/env_comment_en.tmpl templates/env_comment_ru.tmpl
var commentTemplates embed.FS
