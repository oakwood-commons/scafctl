//go:build ignore

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <dist-dir>\n", os.Args[0])
		os.Exit(1)
	}

	distDir := os.Args[1]
	indexPath := filepath.Join(distDir, "index.html")

	readmeContent, err := os.ReadFile("README.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading README.md: %v\n", err)
		os.Exit(1)
	}

	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(readmeContent)

	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)
	readmeHTML := markdown.Render(doc, renderer)

	version := detectVersionFromDist(distDir)
	downloadsHTML := generateDownloadsHTMLFlat(distDir, version)
	readmeHTML = replaceInstallationSection(readmeHTML, downloadsHTML)

	f, err := os.Create(indexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating index.html: %v\n", err)
		os.Exit(1)
	}

	writeHeader(f)

	if _, err := f.Write(readmeHTML); err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "Error writing README content: %v\n", err)
		os.Exit(1)
	}

	writeFooter(f)
	f.Close()

	fmt.Fprintf(os.Stderr, "Generated %s\n", indexPath)
}

// detectVersionFromDist finds the version string from files like
// scafctl_0.1.0_Darwin_arm64.tar.gz
// Falls back to git describe if no archives are found.
func detectVersionFromDist(distDir string) string {
	files, err := os.ReadDir(distDir)
	if err == nil {
		re := regexp.MustCompile(`^scafctl_([^_]+(?:-[^_]+)*)_(?:Darwin|Linux|Windows)_(?:arm64|x86_64)\.(?:tar\.gz|zip)$`)
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			matches := re.FindStringSubmatch(file.Name())
			if len(matches) >= 2 {
				return matches[1]
			}
		}
	}

	// Fallback: try git describe
	if out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output(); err == nil {
		tag := strings.TrimSpace(string(out))
		return strings.TrimPrefix(tag, "v")
	}

	return "dev"
}

// generateDownloadsHTMLFlat generates download links for flat dist/ directory structure.
func generateDownloadsHTMLFlat(distDir, version string) string {
	var sb strings.Builder

	sb.WriteString(`  <div class="downloads">
    <h2>Downloads</h2>
    <div class="version-section">
`)
	sb.WriteString(fmt.Sprintf(`      <h3>%s</h3>
      <table class="download-table">
`, version))

	files, err := os.ReadDir(distDir)
	if err != nil {
		sb.WriteString("      </table>\n    </div>\n  </div>\n")
		return sb.String()
	}

	type platform struct {
		Name    string
		Archive string
	}
	platforms := make(map[string]*platform)

	for _, file := range files {
		name := file.Name()
		if !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".zip") {
			continue
		}
		if strings.Contains(name, "SHA256") {
			continue
		}

		var platKey, platName string
		switch {
		case strings.Contains(name, "Darwin_arm64") || strings.Contains(name, "darwin_arm64"):
			platKey = "darwin-arm64"
			platName = "macOS (Apple Silicon)"
		case strings.Contains(name, "Darwin_x86_64") || strings.Contains(name, "darwin_amd64"):
			platKey = "darwin-amd64"
			platName = "macOS (Intel)"
		case strings.Contains(name, "Linux_arm64") || strings.Contains(name, "linux_arm64"):
			platKey = "linux-arm64"
			platName = "Linux (ARM64)"
		case strings.Contains(name, "Linux_x86_64") || strings.Contains(name, "linux_amd64"):
			platKey = "linux-amd64"
			platName = "Linux (x86_64)"
		case strings.Contains(name, "Windows_arm64") || strings.Contains(name, "windows_arm64"):
			platKey = "windows-arm64"
			platName = "Windows (ARM64)"
		case strings.Contains(name, "Windows_x86_64") || strings.Contains(name, "windows_amd64"):
			platKey = "windows-amd64"
			platName = "Windows (x86_64)"
		default:
			continue
		}

		if platforms[platKey] == nil {
			platforms[platKey] = &platform{Name: platName, Archive: name}
		}
	}

	platKeys := make([]string, 0, len(platforms))
	for k := range platforms {
		platKeys = append(platKeys, k)
	}
	sort.Strings(platKeys)

	for _, key := range platKeys {
		plat := platforms[key]
		sb.WriteString(fmt.Sprintf("        <tr>\n          <td class=\"platform-name\">%s</td>\n          <td class=\"platform-links\"><a href=\"%s\">download</a></td>\n        </tr>\n", plat.Name, plat.Archive))
	}

	sb.WriteString("      </table>\n    </div>\n  </div>\n")
	return sb.String()
}

func writeHeader(w io.Writer) {
	fmt.Fprint(w, `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>scafctl - Configuration as Code with CEL</title>
  <style>
    body { font-family: system-ui, -apple-system, sans-serif; max-width: 900px; margin: 40px auto; padding: 0 20px; line-height: 1.6; color: #333; }
    h1 { color: #2563eb; border-bottom: 2px solid #2563eb; padding-bottom: 10px; }
    h2 { color: #1e40af; margin-top: 30px; }
    h3 { color: #1e3a8a; margin-top: 20px; }
    code { background: #f1f5f9; padding: 2px 6px; border-radius: 3px; font-family: Monaco, Menlo, monospace; font-size: 0.9em; }
    pre { background: #1e293b; color: #e2e8f0; padding: 16px; border-radius: 6px; overflow-x: auto; }
    pre code { background: none; color: inherit; padding: 0; }
    a { color: #2563eb; text-decoration: none; }
    a:hover { text-decoration: underline; }
    table { border-collapse: collapse; width: 100%; margin: 10px 0; }
    th, td { border: 1px solid #e2e8f0; padding: 8px 12px; text-align: left; }
    th { background: #f1f5f9; font-weight: 600; }
    blockquote { border-left: 4px solid #2563eb; margin: 16px 0; padding: 8px 16px; background: #eff6ff; }
    .downloads { background: #eff6ff; padding: 20px; border-radius: 8px; margin: 20px 0; border-left: 4px solid #2563eb; }
    .downloads h2 { margin-top: 0; color: #1e40af; }
    .version-section { margin: 15px 0; padding: 10px; background: white; border-radius: 4px; }
    .version-section h3 { margin: 0 0 10px 0; color: #1e3a8a; font-size: 1.1em; }
    .download-table { width: 100%; border-collapse: collapse; border: none; }
    .download-table td { padding: 6px 8px; border: none; }
    .platform-name { font-weight: 500; color: #1e3a8a; width: 200px; }
    .platform-links a { color: #2563eb; text-decoration: none; font-weight: 500; margin: 0 4px; }
    .platform-links a:hover { text-decoration: underline; }
  </style>
</head>
<body>
`)
}

func writeFooter(w io.Writer) {
	fmt.Fprint(w, "</body>\n</html>\n")
}

func replaceInstallationSection(htmlContent []byte, downloadsHTML string) []byte {
	content := string(htmlContent)

	installStart := strings.Index(content, `<h2 id="install">`)
	if installStart == -1 {
		installStart = strings.Index(content, `<h2 id="installation">`)
	}
	if installStart == -1 {
		return htmlContent
	}

	nextH2 := strings.Index(content[installStart+20:], `<h2 id="`)
	if nextH2 == -1 {
		return htmlContent
	}
	nextH2 += installStart + 20

	replacement := `<h2 id="installation">Installation</h2>

` + downloadsHTML + `
<h3>Homebrew (macOS / Linux)</h3>
<pre><code class="language-bash">brew install oakwood-commons/tap/scafctl
</code></pre>

<h3>Manual Install</h3>
<p>Extract the archive and move the binary to your PATH:</p>
<pre><code class="language-bash"># macOS / Linux
tar -xzf scafctl_*.tar.gz
sudo mv scafctl /usr/local/bin/

# Windows
# Extract the .zip file and add scafctl.exe to your PATH
</code></pre>

<blockquote><p><strong>macOS note:</strong> You may need to remove the quarantine attribute before running:</p>
<pre><code class="language-bash">xattr -dr 'com.apple.quarantine' /usr/local/bin/scafctl
</code></pre></blockquote>

<h3>From Source</h3>
<pre><code class="language-bash">go install github.com/oakwood-commons/scafctl/cmd/scafctl@latest
</code></pre>

`

	result := content[:installStart] + replacement + content[nextH2:]
	return []byte(result)
}
