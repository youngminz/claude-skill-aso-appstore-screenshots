package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type screenshotItem struct {
	Path        string `json:"path"`
	FileName    string `json:"fileName"`
	Title       string `json:"title"`
	Locale      string `json:"locale"`
	Device      string `json:"device"`
	DeviceLabel string `json:"deviceLabel"`
	Slot        string `json:"slot"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Size        int64  `json:"size"`
	ModTime     string `json:"modTime"`
}

type countOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type galleryResponse struct {
	Root      string           `json:"root"`
	ScannedAt string           `json:"scannedAt"`
	Total     int              `json:"total"`
	Locales   []countOption    `json:"locales"`
	Devices   []countOption    `json:"devices"`
	Items     []screenshotItem `json:"items"`
}

func main() {
	defaultDir := "ios/fastlane/screenshots"
	if _, err := os.Stat(defaultDir); err != nil {
		defaultDir = "screenshots"
	}

	dirFlag := flag.String("dir", defaultDir, "screenshot directory to scan")
	addrFlag := flag.String("addr", "127.0.0.1:8787", "address to listen on")
	openFlag := flag.Bool("open", false, "open the gallery in a browser")
	flag.Parse()

	rootAbs, err := filepath.Abs(*dirFlag)
	if err != nil {
		log.Fatalf("resolve directory: %v", err)
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		log.Fatalf("read directory %s: %v", rootAbs, err)
	}
	if !info.IsDir() {
		log.Fatalf("%s is not a directory", rootAbs)
	}

	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	})
	mux.HandleFunc("/api/screenshots", func(w http.ResponseWriter, r *http.Request) {
		gallery, err := scanScreenshots(rootAbs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(gallery)
	})
	mux.HandleFunc("/image", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		filePath, err := safeJoin(rootAbs, rootReal, rel)
		if err != nil {
			http.Error(w, "invalid image path", http.StatusBadRequest)
			return
		}
		if !isImageFile(filePath) {
			http.Error(w, "unsupported image type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		http.ServeFile(w, r, filePath)
	})

	ln, err := net.Listen("tcp", *addrFlag)
	if err != nil {
		log.Fatalf("listen on %s: %v", *addrFlag, err)
	}

	url := localURL(ln.Addr())
	fmt.Printf("iOS screenshot gallery\n")
	fmt.Printf("Directory: %s\n", rootAbs)
	fmt.Printf("URL: %s\n", url)
	fmt.Printf("Stop with Ctrl-C\n")

	if *openFlag {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := openURL(url); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}

	if err := http.Serve(ln, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func scanScreenshots(root string) (galleryResponse, error) {
	items := make([]screenshotItem, 0, 512)
	localeCounts := map[string]int{}
	deviceCounts := map[string]int{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !isImageFile(name) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		dirParts := parts[:maxInt(0, len(parts)-1)]

		locale := "root"
		device := "root"
		if len(dirParts) > 0 && dirParts[0] != "" {
			locale = dirParts[0]
		}
		if len(dirParts) > 1 && dirParts[1] != "" {
			device = dirParts[1]
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		width, height := imageSize(path)
		order := leadingNumber(name)
		slot := ""
		if order >= 0 {
			slot = fmt.Sprintf("%02d", order)
		}

		items = append(items, screenshotItem{
			Path:        rel,
			FileName:    name,
			Title:       titleFromFileName(name),
			Locale:      locale,
			Device:      device,
			DeviceLabel: deviceLabel(device),
			Slot:        slot,
			Width:       width,
			Height:      height,
			Size:        info.Size(),
			ModTime:     info.ModTime().Format(time.RFC3339),
		})
		localeCounts[locale]++
		deviceCounts[device]++
		return nil
	})
	if err != nil {
		return galleryResponse{}, err
	}

	sort.Slice(items, func(i, j int) bool {
		a := items[i]
		b := items[j]
		if a.Locale != b.Locale {
			return a.Locale < b.Locale
		}
		if a.Device != b.Device {
			return a.Device < b.Device
		}
		ai := sortSlot(a)
		bi := sortSlot(b)
		if ai != bi {
			return ai < bi
		}
		return a.FileName < b.FileName
	})

	return galleryResponse{
		Root:      root,
		ScannedAt: time.Now().Format(time.RFC3339),
		Total:     len(items),
		Locales:   countOptions(localeCounts, nil),
		Devices:   countOptions(deviceCounts, deviceLabel),
		Items:     items,
	}, nil
}

func countOptions(counts map[string]int, labelFn func(string) string) []countOption {
	options := make([]countOption, 0, len(counts))
	for value, count := range counts {
		label := value
		if labelFn != nil {
			label = labelFn(value)
		}
		options = append(options, countOption{
			Value: value,
			Label: label,
			Count: count,
		})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Label != options[j].Label {
			return options[i].Label < options[j].Label
		}
		return options[i].Value < options[j].Value
	})
	return options
}

func imageSize(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func isImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func safeJoin(rootAbs string, rootReal string, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty path")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes root")
	}

	full := filepath.Join(rootAbs, clean)
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !isWithin(fullAbs, rootAbs) {
		return "", errors.New("path escapes root")
	}

	fullReal, err := filepath.EvalSymlinks(fullAbs)
	if err == nil && !isWithin(fullReal, rootReal) {
		return "", errors.New("symlink escapes root")
	}
	return fullAbs, nil
}

func isWithin(path string, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func leadingNumber(name string) int {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	digits := strings.Builder{}
	for _, r := range base {
		if r < '0' || r > '9' {
			break
		}
		digits.WriteRune(r)
	}
	if digits.Len() == 0 {
		return -1
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil {
		return -1
	}
	return n
}

func sortSlot(item screenshotItem) int {
	if item.Slot == "" {
		return 9999
	}
	n, err := strconv.Atoi(item.Slot)
	if err != nil {
		return 9999
	}
	return n
}

func titleFromFileName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = trimLeadingNumber(base)

	parts := strings.SplitN(base, "_", 2)
	if len(parts) == 2 && looksLikeUUID(parts[0]) {
		base = trimLeadingNumber(parts[1])
	}

	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.Join(strings.Fields(base), " ")
	if base == "" {
		return name
	}
	return base
}

func trimLeadingNumber(value string) string {
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i < len(value) && (value[i] == '_' || value[i] == '-' || value[i] == ' ') {
		i++
	}
	return value[i:]
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isDigit := r >= '0' && r <= '9'
			isHexLower := r >= 'a' && r <= 'f'
			isHexUpper := r >= 'A' && r <= 'F'
			if !isDigit && !isHexLower && !isHexUpper {
				return false
			}
		}
	}
	return true
}

func deviceLabel(device string) string {
	switch device {
	case "APP_IPHONE_65":
		return "iPhone 6.5 inch"
	case "APP_IPHONE_55":
		return "iPhone 5.5 inch"
	case "APP_IPAD_PRO_3GEN_129":
		return "iPad Pro 12.9 inch 3rd gen"
	case "APP_IPAD_PRO_129":
		return "iPad Pro 12.9 inch"
	case "root":
		return "Root"
	default:
		label := strings.ReplaceAll(device, "_", " ")
		label = strings.ReplaceAll(label, "-", " ")
		label = strings.Join(strings.Fields(label), " ")
		return label
	}
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func localURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "::" || host == "[::]" || host == "0.0.0.0" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

var pageHTML = strings.TrimSpace(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>iOS Screenshot Gallery</title>
  <style>
    :root {
      --bg: #f6f3ee;
      --surface: #fffdf9;
      --ink: #1e2328;
      --muted: #687076;
      --line: #ddd6cc;
      --accent: #b73e2d;
      --green: #2c7a58;
      --gold: #a06b15;
      --shadow: 0 14px 36px rgba(48, 41, 34, 0.12);
      --thumb-h: 360px;
    }

    * {
      box-sizing: border-box;
    }

    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
    }

    button,
    input,
    select {
      font: inherit;
    }

    .shell {
      min-height: 100vh;
      display: grid;
      grid-template-rows: auto 1fr;
    }

    .topbar {
      position: sticky;
      top: 0;
      z-index: 30;
      border-bottom: 1px solid var(--line);
      background: rgba(255, 253, 249, 0.94);
      backdrop-filter: blur(16px);
    }

    .topbar-inner {
      max-width: 1760px;
      margin: 0 auto;
      padding: 14px 22px;
      display: grid;
      grid-template-columns: minmax(210px, auto) 1fr;
      gap: 16px;
      align-items: center;
    }

    h1 {
      margin: 0;
      font-size: 19px;
      line-height: 1.1;
      font-weight: 760;
    }

    .subline {
      margin-top: 5px;
      color: var(--muted);
      font-size: 12px;
      max-width: 360px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .filters {
      display: grid;
      grid-template-columns: minmax(220px, 1.3fr) minmax(140px, 0.7fr) minmax(180px, 0.9fr);
      gap: 10px;
      align-items: center;
    }

    .control {
      min-width: 0;
      position: relative;
    }

    .control label {
      display: block;
      margin: 0 0 5px;
      color: var(--muted);
      font-size: 11px;
      font-weight: 700;
      text-transform: uppercase;
    }

    input,
    select {
      width: 100%;
      height: 38px;
      border: 1px solid var(--line);
      border-radius: 7px;
      background: #ffffff;
      color: var(--ink);
      padding: 0 11px;
      outline: none;
    }

    .control.has-icon input {
      padding-left: 35px;
    }

    .field-icon {
      position: absolute;
      left: 11px;
      bottom: 10px;
      width: 16px;
      height: 16px;
      color: var(--muted);
      pointer-events: none;
    }

    input:focus,
    select:focus,
    button:focus-visible {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(183, 62, 45, 0.17);
    }

    .content {
      max-width: 1760px;
      width: 100%;
      margin: 0 auto;
      padding: 22px;
    }

    .status {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      margin-bottom: 16px;
    }

    .pill {
      display: inline-flex;
      align-items: center;
      min-height: 28px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: rgba(255, 255, 255, 0.68);
      padding: 4px 10px;
      color: var(--muted);
      font-size: 12px;
      font-weight: 680;
      white-space: nowrap;
    }

    .pill strong {
      color: var(--ink);
      margin-right: 5px;
    }

    .pill.green strong {
      color: var(--green);
    }

    .pill.gold strong {
      color: var(--gold);
    }

    .sections {
      display: grid;
      gap: 26px;
    }

    .section {
      min-width: 0;
    }

    .section-head {
      display: flex;
      justify-content: space-between;
      align-items: end;
      gap: 12px;
      margin-bottom: 10px;
      padding-bottom: 8px;
      border-bottom: 1px solid var(--line);
    }

    .section-title {
      margin: 0;
      font-size: 16px;
      line-height: 1.25;
      font-weight: 760;
    }

    .section-meta {
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
    }

    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
      gap: 16px;
      align-items: start;
    }

    .shot {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
      background: var(--surface);
      box-shadow: 0 1px 0 rgba(255, 255, 255, 0.75);
      cursor: zoom-in;
      padding: 0;
      text-align: left;
    }

    .shot:hover {
      border-color: rgba(183, 62, 45, 0.55);
      box-shadow: var(--shadow);
      transform: translateY(-1px);
    }

    .frame {
      height: var(--thumb-h);
      display: flex;
      align-items: center;
      justify-content: center;
      background:
        linear-gradient(45deg, rgba(0,0,0,0.035) 25%, transparent 25%),
        linear-gradient(-45deg, rgba(0,0,0,0.035) 25%, transparent 25%),
        linear-gradient(45deg, transparent 75%, rgba(0,0,0,0.035) 75%),
        linear-gradient(-45deg, transparent 75%, rgba(0,0,0,0.035) 75%);
      background-size: 18px 18px;
      background-position: 0 0, 0 9px, 9px -9px, -9px 0;
    }

    .frame img {
      display: block;
      max-width: 100%;
      max-height: 100%;
      object-fit: contain;
      background: #ffffff;
    }

    .caption {
      padding: 9px 10px 10px;
      display: grid;
      gap: 4px;
    }

    .caption-title {
      min-width: 0;
      color: var(--ink);
      font-size: 12px;
      line-height: 1.25;
      font-weight: 720;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .caption-meta {
      min-width: 0;
      color: var(--muted);
      font-size: 11px;
      line-height: 1.25;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .empty {
      padding: 34px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(255, 255, 255, 0.62);
      color: var(--muted);
    }

    .modal {
      position: fixed;
      inset: 0;
      z-index: 80;
      display: none;
      background: rgba(18, 20, 22, 0.88);
      color: #ffffff;
    }

    .modal.open {
      display: grid;
      grid-template-rows: auto 1fr auto;
    }

    .modal-bar {
      min-width: 0;
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 12px;
      align-items: center;
      padding: 12px 14px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.14);
    }

    .modal-title {
      min-width: 0;
    }

    .modal-title strong,
    .modal-title span {
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .modal-title strong {
      font-size: 14px;
    }

    .modal-title span {
      margin-top: 3px;
      color: rgba(255, 255, 255, 0.68);
      font-size: 12px;
    }

    .modal-actions {
      display: flex;
      gap: 8px;
    }

    .modal-actions a,
    .modal-actions button {
      height: 36px;
      border: 1px solid rgba(255, 255, 255, 0.28);
      border-radius: 7px;
      background: rgba(255, 255, 255, 0.08);
      color: #ffffff;
      padding: 0 12px;
      text-decoration: none;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      cursor: pointer;
    }

    .modal-stage {
      min-height: 0;
      display: grid;
      grid-template-columns: auto 1fr auto;
      align-items: center;
      gap: 10px;
      padding: 12px;
    }

    .modal-stage img {
      display: block;
      max-width: 100%;
      max-height: calc(100vh - 132px);
      justify-self: center;
      object-fit: contain;
      box-shadow: 0 20px 80px rgba(0, 0, 0, 0.38);
      background: #ffffff;
    }

    .nav-button {
      width: 48px;
      height: 64px;
      border: 1px solid rgba(255, 255, 255, 0.2);
      border-radius: 8px;
      background: rgba(255, 255, 255, 0.08);
      color: #ffffff;
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      padding: 0;
    }

    .lucide {
      width: 17px;
      height: 17px;
      stroke-width: 2;
    }

    .nav-button .lucide {
      width: 26px;
      height: 26px;
    }

    .visually-hidden {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
      border: 0;
    }

    .modal-foot {
      padding: 10px 14px;
      color: rgba(255, 255, 255, 0.7);
      font-size: 12px;
      border-top: 1px solid rgba(255, 255, 255, 0.14);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    @media (max-width: 980px) {
      .topbar-inner {
        grid-template-columns: 1fr;
      }

      .filters {
        grid-template-columns: 1fr 1fr;
      }

    }

    @media (max-width: 620px) {
      .topbar-inner,
      .content {
        padding-left: 14px;
        padding-right: 14px;
      }

      .filters {
        grid-template-columns: 1fr;
      }

      .modal-stage {
        grid-template-columns: 1fr;
      }

      .nav-button {
        display: none;
      }
    }
  </style>
</head>
<body>
  <div class="shell">
    <header class="topbar">
      <div class="topbar-inner">
        <div>
          <h1>iOS Screenshot Gallery</h1>
          <div id="rootPath" class="subline">Loading...</div>
        </div>

        <div class="filters">
          <div class="control has-icon">
            <label for="search">Search</label>
            <i data-lucide="search" class="field-icon" aria-hidden="true"></i>
            <input id="search" type="search" placeholder="locale, device, file name">
          </div>
          <div class="control">
            <label for="locale">Locale</label>
            <select id="locale"></select>
          </div>
          <div class="control">
            <label for="device">Device</label>
            <select id="device"></select>
          </div>
        </div>

      </div>
    </header>

    <main class="content">
      <div id="status" class="status"></div>
      <div id="sections" class="sections"></div>
    </main>
  </div>

  <div id="modal" class="modal" aria-hidden="true">
    <div class="modal-bar">
      <div class="modal-title">
        <strong id="modalTitle"></strong>
        <span id="modalMeta"></span>
      </div>
      <div class="modal-actions">
        <a id="modalOpen" target="_blank" rel="noreferrer" title="Open image">
          <i data-lucide="external-link" aria-hidden="true"></i>
          <span>Open</span>
        </a>
        <button id="modalClose" type="button" title="Close image">
          <i data-lucide="x" aria-hidden="true"></i>
          <span>Close</span>
        </button>
      </div>
    </div>
    <div class="modal-stage">
      <button id="prev" type="button" class="nav-button" aria-label="Previous image" title="Previous image">
        <i data-lucide="chevron-left" aria-hidden="true"></i>
      </button>
      <img id="modalImage" alt="">
      <button id="next" type="button" class="nav-button" aria-label="Next image" title="Next image">
        <i data-lucide="chevron-right" aria-hidden="true"></i>
      </button>
    </div>
    <div id="modalPath" class="modal-foot"></div>
  </div>

  <script src="https://unpkg.com/lucide@1.16.0/dist/umd/lucide.min.js"></script>
  <script>
    var state = {
      items: [],
      visible: [],
      locale: "all",
      device: "all",
      query: "",
      activeIndex: -1
    };

    var els = {
      rootPath: document.getElementById("rootPath"),
      status: document.getElementById("status"),
      sections: document.getElementById("sections"),
      search: document.getElementById("search"),
      locale: document.getElementById("locale"),
      device: document.getElementById("device"),
      modal: document.getElementById("modal"),
      modalTitle: document.getElementById("modalTitle"),
      modalMeta: document.getElementById("modalMeta"),
      modalImage: document.getElementById("modalImage"),
      modalPath: document.getElementById("modalPath"),
      modalOpen: document.getElementById("modalOpen"),
      modalClose: document.getElementById("modalClose"),
      prev: document.getElementById("prev"),
      next: document.getElementById("next")
    };

    function imageURL(item) {
      return "/image?path=" + encodeURIComponent(item.path);
    }

    function createIcons() {
      if (window.lucide && typeof window.lucide.createIcons === "function") {
        window.lucide.createIcons();
      }
    }

    function escapeHTML(value) {
      return String(value).replace(/[&<>"']/g, function(ch) {
        return {
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          "\"": "&quot;",
          "'": "&#39;"
        }[ch];
      });
    }

    function bytes(value) {
      if (!value) return "";
      var units = ["B", "KB", "MB", "GB"];
      var n = value;
      var i = 0;
      while (n >= 1024 && i < units.length - 1) {
        n = n / 1024;
        i++;
      }
      return (i === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + units[i];
    }

    function dimensions(item) {
      if (!item.width || !item.height) return "";
      return item.width + " x " + item.height;
    }

    function populateSelect(select, options, allLabel) {
      var current = select.value || "all";
      var html = "<option value=\"all\">" + escapeHTML(allLabel) + "</option>";
      options.forEach(function(option) {
        html += "<option value=\"" + escapeHTML(option.value) + "\">" +
          escapeHTML(option.label + " (" + option.count + ")") +
          "</option>";
      });
      select.innerHTML = html;
      select.value = Array.from(select.options).some(function(option) {
        return option.value === current;
      }) ? current : "all";
    }

    function sectionKey(item) {
      return item.locale + " / " + item.deviceLabel + "||" + item.locale + "/" + item.device;
    }

    function filteredItems() {
      var q = state.query.trim().toLowerCase();
      return state.items.filter(function(item) {
        if (state.locale !== "all" && item.locale !== state.locale) return false;
        if (state.device !== "all" && item.device !== state.device) return false;
        if (!q) return true;
        var haystack = [
          item.path,
          item.fileName,
          item.title,
          item.locale,
          item.device,
          item.deviceLabel,
          item.slot
        ].join(" ").toLowerCase();
        return haystack.indexOf(q) !== -1;
      });
    }

    function renderStatus(total, visible) {
      var locales = new Set(visible.map(function(item) { return item.locale; })).size;
      var devices = new Set(visible.map(function(item) { return item.device; })).size;
      els.status.innerHTML =
        "<span class=\"pill\"><strong>" + visible.length + "</strong> visible</span>" +
        "<span class=\"pill\"><strong>" + total + "</strong> total</span>" +
        "<span class=\"pill green\"><strong>" + locales + "</strong> locales</span>" +
        "<span class=\"pill gold\"><strong>" + devices + "</strong> devices</span>";
    }

    function render() {
      var visible = filteredItems();
      state.visible = visible;
      renderStatus(state.items.length, visible);

      if (visible.length === 0) {
        els.sections.innerHTML = "<div class=\"empty\">No screenshots match the current filters.</div>";
        return;
      }

      var sections = new Map();
      visible.forEach(function(item, index) {
        var key = sectionKey(item);
        if (!sections.has(key)) sections.set(key, []);
        item.visibleIndex = index;
        sections.get(key).push(item);
      });

      var markup = "";
      sections.forEach(function(items, key) {
        var title = key.split("||")[0];
        markup += "<section class=\"section\">" +
          "<div class=\"section-head\">" +
          "<h2 class=\"section-title\">" + escapeHTML(title) + "</h2>" +
          "<div class=\"section-meta\">" + items.length + " images</div>" +
          "</div><div class=\"grid\">";

        items.forEach(function(item) {
          var meta = [
            item.locale,
            item.deviceLabel,
            dimensions(item),
            bytes(item.size)
          ].filter(Boolean).join(" | ");
          markup += "<button class=\"shot\" type=\"button\" data-index=\"" + item.visibleIndex + "\">" +
            "<div class=\"frame\">" +
            "<img loading=\"lazy\" src=\"" + imageURL(item) + "\" alt=\"" + escapeHTML(item.title) + "\">" +
            "</div>" +
            "<div class=\"caption\">" +
            "<div class=\"caption-title\">" + escapeHTML((item.slot ? item.slot + " " : "") + item.title) + "</div>" +
            "<div class=\"caption-meta\">" + escapeHTML(meta) + "</div>" +
            "</div></button>";
        });

        markup += "</div></section>";
      });
      els.sections.innerHTML = markup;

      els.sections.querySelectorAll(".shot").forEach(function(button) {
        button.addEventListener("click", function() {
          openModal(Number(button.dataset.index));
        });
      });
    }

    function openModal(index) {
      if (index < 0 || index >= state.visible.length) return;
      state.activeIndex = index;
      var item = state.visible[index];
      var meta = [
        item.locale,
        item.deviceLabel,
        dimensions(item),
        bytes(item.size)
      ].filter(Boolean).join(" | ");
      var url = imageURL(item);
      els.modalTitle.textContent = (item.slot ? item.slot + " " : "") + item.title;
      els.modalMeta.textContent = meta;
      els.modalImage.src = url;
      els.modalImage.alt = item.title;
      els.modalPath.textContent = item.path;
      els.modalOpen.href = url;
      els.modal.classList.add("open");
      els.modal.setAttribute("aria-hidden", "false");
    }

    function closeModal() {
      els.modal.classList.remove("open");
      els.modal.setAttribute("aria-hidden", "true");
      els.modalImage.src = "";
      state.activeIndex = -1;
    }

    function moveModal(delta) {
      if (state.activeIndex < 0) return;
      var nextIndex = state.activeIndex + delta;
      if (nextIndex < 0) nextIndex = state.visible.length - 1;
      if (nextIndex >= state.visible.length) nextIndex = 0;
      openModal(nextIndex);
    }

    async function load() {
      try {
        var response = await fetch("/api/screenshots", { cache: "no-store" });
        if (!response.ok) throw new Error(await response.text());
        var data = await response.json();
        state.items = data.items || [];
        els.rootPath.textContent = data.root || "";
        populateSelect(els.locale, data.locales || [], "All locales");
        populateSelect(els.device, data.devices || [], "All devices");
        state.locale = els.locale.value;
        state.device = els.device.value;
        render();
      } catch (error) {
        els.sections.innerHTML = "<div class=\"empty\">" + escapeHTML(error.message || error) + "</div>";
      }
    }

    els.search.addEventListener("input", function(event) {
      state.query = event.target.value;
      render();
    });

    els.locale.addEventListener("change", function(event) {
      state.locale = event.target.value;
      render();
    });

    els.device.addEventListener("change", function(event) {
      state.device = event.target.value;
      render();
    });

    els.modalClose.addEventListener("click", closeModal);
    els.prev.addEventListener("click", function() { moveModal(-1); });
    els.next.addEventListener("click", function() { moveModal(1); });

    els.modal.addEventListener("click", function(event) {
      var target = event.target;
      if (!(target instanceof Element)) return;
      if (target === els.modalImage || target.closest(".modal-actions") || target.closest(".nav-button")) return;
      closeModal();
    });

    document.addEventListener("keydown", function(event) {
      if (!els.modal.classList.contains("open")) return;
      if (event.key === "Escape") closeModal();
      if (event.key === "ArrowLeft") moveModal(-1);
      if (event.key === "ArrowRight") moveModal(1);
    });

    createIcons();
    load();
  </script>
</body>
</html>`)
