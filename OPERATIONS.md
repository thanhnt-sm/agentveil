# 🛡️ AgentVeil — Hướng dẫn Vận hành

## Tắt / Bật

### Proxy (bảo vệ AI traffic)

```bash
# Tắt proxy
./setup.sh --stop
# hoặc
launchctl unload ~/Library/LaunchAgents/com.agentveil.proxy.plist

# Bật proxy
./setup.sh --start
# hoặc
launchctl load ~/Library/LaunchAgents/com.agentveil.proxy.plist

# Restart (rebuild + khởi động lại)
./setup.sh --restart

# Kiểm tra trạng thái
./setup.sh --status
```

### Watchdog (auto health-check + skill scan mỗi 15 phút)

```bash
# Tắt watchdog
launchctl unload ~/Library/LaunchAgents/com.agentveil.watchdog.plist

# Bật watchdog
launchctl load ~/Library/LaunchAgents/com.agentveil.watchdog.plist

# Chạy thủ công 1 lần
bash ~/.agentveil/watchdog.sh

# Xem report
cat ~/.agentveil/reports/watchdog-*.json | python3 -m json.tool | tail -20
```

### Dashboard (web UI)

```bash
# Dashboard chạy cùng proxy, không cần tắt/bật riêng
# Truy cập: http://localhost:8080/dashboard
open http://localhost:8080/dashboard
```

---

## Cài đặt

### Cài mới từ đầu

```bash
# 1. Clone repo
git clone https://github.com/vurakit/agentveil.git && cd agentveil

# 2. Setup tự động (build + cài Redis + launchd + env vars)
./setup.sh

# 3. Áp dụng env vars
source ~/.zshrc

# 4. Kiểm tra
./setup.sh --status
curl http://localhost:8080/health
```

### Cài watchdog (tự động scan skill + health check)

```bash
# Copy plist vào LaunchAgents
cp com.agentveil.watchdog.plist ~/Library/LaunchAgents/

# Load
launchctl load ~/Library/LaunchAgents/com.agentveil.watchdog.plist
```

### Cập nhật code

```bash
cd /Volumes/Data/101.AI/GitHub/agentveil
git pull
./setup.sh --restart
```

---

## Gỡ cài đặt (Xoá hoàn toàn)

```bash
# 1. Tắt tất cả services
launchctl unload ~/Library/LaunchAgents/com.agentveil.proxy.plist 2>/dev/null
launchctl unload ~/Library/LaunchAgents/com.agentveil.watchdog.plist 2>/dev/null

# 2. Xoá plist files
rm -f ~/Library/LaunchAgents/com.agentveil.proxy.plist
rm -f ~/Library/LaunchAgents/com.agentveil.watchdog.plist

# 3. Xoá thư mục cài đặt
rm -rf ~/.agentveil

# 4. Xoá env vars khỏi shell profile
# Mở ~/.zshrc và xoá đoạn "# === Agent Veil ===" đến "# === End Agent Veil ==="
nano ~/.zshrc

# 5. Áp dụng
source ~/.zshrc

# Hoặc dùng lệnh tự động:
cd /Volumes/Data/101.AI/GitHub/agentveil && ./setup.sh --uninstall
```

---

## Lệnh nhanh (cheat sheet)

| Thao tác | Lệnh |
|----------|-------|
| Kiểm tra trạng thái | `./setup.sh --status` |
| Bật proxy | `./setup.sh --start` |
| Tắt proxy | `./setup.sh --stop` |
| Restart + rebuild | `./setup.sh --restart` |
| Xem logs | `./setup.sh --logs` |
| Mở dashboard | `open http://localhost:8080/dashboard` |
| Scan PII | `agentveil scan "text"` |
| Audit skill | `agentveil audit path/to/SKILL.md` |
| Compliance check | `agentveil compliance check --framework all` |
| Bật watchdog | `launchctl load ~/Library/LaunchAgents/com.agentveil.watchdog.plist` |
| Tắt watchdog | `launchctl unload ~/Library/LaunchAgents/com.agentveil.watchdog.plist` |
| Xoá hoàn toàn | `./setup.sh --uninstall` |

---

## Cấu trúc thư mục

```
~/.agentveil/
├── bin/
│   ├── agentveil-proxy     # Proxy binary
│   └── agentveil           # CLI tool
├── logs/
│   ├── proxy.log           # Proxy logs
│   ├── watchdog.log        # Watchdog logs
│   └── watchdog-*.log      # Watchdog stdout/stderr
├── reports/
│   └── watchdog-*.json     # Scan reports (giữ 48 bản mới nhất)
├── .env                    # Config (encryption key, target URL, etc.)
├── router.yaml             # Multi-provider routing config
└── watchdog.sh             # Automation script
```
