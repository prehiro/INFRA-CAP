# INFRA-CAP — Project Plan

> Status: DRAFT v2 — review sebelum eksekusi.
> INFRA-CAP adalah platform manajemen infrastruktur/IT internal. **v1 fokus: modul Software License.** Modul lain menyusul — arsitektur wajib modular/extensible.

**Goal:** Web app internal 5–10 user; v1 = pengelolaan record software license (Maker, Software Name, Version, License Key, Activation Key, Assigned To, Device Hostname, Device SN, Section, PO No, Status, Activated On, Expiry Date) + dashboard ringkasan + audit trail, dengan fondasi yang mudah diperluas ke modul lain.

**Architecture:** Monolith server-rendered (Server-Driven UI). Satu binary Go, HTML templates + HTMX partials, Tailwind/DaisyUI via standalone CLI, Alpine.js untuk mikro-interaksi. Modular by package: setiap fitur = paket `internal/modules/<nama>` dengan routes-nya sendiri yang diregistrasi ke router utama — menambah modul baru tidak menyentuh modul lama.

**Tech Stack:**
- Backend: Go 1.27 stdlib `net/http`, routing pattern modern (`GET /licenses/{id}`)
- DB driver: `github.com/microsoft/go-mssqldb` (SQL Server 2022 dev & prod)
- Frontend: HTMX + Tailwind CSS + DaisyUI (class-based, cocok dengan HTMX swap; standalone Tailwind CLI binary, tanpa npm) + Alpine.js
- Auth: session cookie, bcrypt
- Testing: stdlib `testing` + httptest

---

## 1. Environment & Konvensi

| Item | Dev | Prod |
|---|---|---|
| OS | Debian 13 | Windows Server 2022 |
| SQL Server | 2022 Express @192.168.4.3:1433 | 2022 |
| Run | `make run` port :1112 (dev) | Windows Service native (`infracap.exe`, dua-mode), port :1112 |

Konvensi:
- **Self-contained project dir**: seluruh deps & toolchain berada di dalam `/home/hiro/Project/INFRA-CAP`, tidak ada instalasi global ke sistem:
  - Go modules cache lokal: `GOFLAGS=-mod=mod` + `GOMODCACHE=$PWD/.gomodcache`, `GOPATH=$PWD/.gopath` (diset lewat Makefile/`.envrc`)
  - Tailwind standalone CLI binary disimpan di `tools/tailwindcss` (di-repo atau di-download oleh `make tools`)
  - Alpine.js & HTMX di-vendor ke `web/static/js/` (file statis biasa, bukan CDN)
  - `.gitignore`: `.env`, `.gopath/`, `.gomodcache/`, binary build
- Repo layout (modular):
  ```
  INFRA-CAP/
    cmd/server/main.go            # wiring: config → db → register modules → listen
    internal/
      config/config.go
      db/db.go                    # koneksi + query helpers
      migrations/runner.go        # eksekusi migrations/*.sql urut
      auth/                       # login, session, middleware role
      web/                        # render helper, flash msg, htmx utils
      modules/
        dashboard/module.go       # tiap modul expose RegisterRoutes(mux)
        licenses/                 # v1 utama: handlers.go, model.go, queries.go
    web/templates/{layouts,pages,partials}/
    web/static/{css,js}/
    migrations/*.sql              # 0001_users.sql, 0010_licenses.sql, ...
    Makefile                      # run / build / tailwind-watch / test
  ```
- Config via env vars (`INFRACAP_DB_DSN`, `INFRACAP_ADDR` default `:1112`, `INFRACAP_SESSION_SECRET`). `.env` lokal saja, di-gitignore.

- Conventional commits.

## 2. Fase Eksekusi

### Fase 0 — Bootstrap & Fondasi Modular
- Task 0.1: `git init`, struktur folder di atas, `go mod init infracap`
- Task 0.2: `internal/config` — load env
- Task 0.3: Koneksi MSSQL + health endpoint `GET /healthz` (ping DB)
- Task 0.4: Migration runner sederhana (`schema_migrations` table)
- Task 0.5: Kontrak modul: interface `type Module interface { RegisterRoutes(mux *http.ServeMux) }`; main.go mengumpulkan semua modul
- Task 0.6: Tailwind CLI (download ke `tools/`), DaisyUI setup via `@plugin`, HTMX + Alpine.js vendored ke `web/static/js/`, base layout template dengan **collapsible sidebar navigation** (toggle expand/collapse via Alpine; state collapsed = icon-only + tooltip; menu item per modul; di mobile jadi off-canvas drawer)
- Verify: `curl localhost:1112/healthz` → `{"db":"ok"}`

### Fase 1 — Auth & Users
- `users(id, username, password_hash, full_name, role [admin|user], is_active)` + `sessions`
- Login/logout, middleware `RequireAuth` / `RequireRole`
- Admin CRUD users
- Menu navigasi siap menampung modul-modul mendatang (sidebar collapsible dari Task 0.6)

### Fase 2 — Modul Software License (v1 core)
Field per record (sesuai spec Hiro): Maker, Software Name, Version, License Key, Activation Key, Assigned To, Device Hostname, Device SN, Section, PO No, Status (In use/Available/Expired/Retired), Activated On (DD-MM-YY), Expiry Date.

Skema:
```
licenses(
  id INT IDENTITY PK,
  maker NVARCHAR(150) NOT NULL,
  software_name NVARCHAR(200) NOT NULL,
  version NVARCHAR(50),
  license_key NVARCHAR(300),
  activation_key NVARCHAR(300),
  assigned_to NVARCHAR(150),
  device_hostname NVARCHAR(100),
  device_sn NVARCHAR(100),
  section NVARCHAR(100),
  po_no NVARCHAR(100),
  status NVARCHAR(20) NOT NULL DEFAULT 'Available',  -- In use|Available|Expired|Retired
  activated_on DATE NULL,
  expiry_date DATE NULL,
  created_at DATETIME2 DEFAULT SYSUTCDATETIME(),
  updated_at DATETIME2 DEFAULT SYSUTCDATETIME()
)
```
Halaman:
- **Dashboard**: kartu ringkasan (total, in use, available, expiring ≤30 hari) + tabel "expiring soon"
- **License Manager**: tabel dengan search + filter (status, section, expiry range), sort kolom, pagination server-side
- Create/Edit: modal form HTMX, validasi server-side (status enum, expiry_date ≥ activated_on); date input format tampil DD-MM-YY
- Delete: konfirmasi modal → **soft delete** (status → Retired), tanpa DELETE fisik
- Export Excel: endpoint `GET /licenses/export` (excelize, pure Go) membawa filter aktif; header bold + freeze panes + auto-width
- Index nonclustered awal: `status`, `expiry_date`, `section`

### Fase 2b — Audit Trail (fondasi lintas modul)
- Tabel `audit_log(id, actor_id, actor_name, action [create|update|delete|login|export], entity, entity_id, changes NVARCHAR(MAX) /*JSON before/after*/, ip, created_at)` + index (entity, entity_id), (created_at DESC)
- Helper `audit.Log(r, entity, id, action, before, after)` dipanggil di layer handler — dipakai semua modul sekarang & mendatang
- Halaman admin `/audit`: tabel terbaru dulu, filter (actor, action, entity, date range), pagination server-side; row expandable untuk detail before/after
- Login gagal/sukses & export juga dicatat

### Fase 3 — Polish & Ekstensibilitas
- Responsive pass, loading indicator `hx-indicator`, toast Alpine, empty states
- Refactor check: memastikan pola modul licenses bersih sebagai template modul berikutnya

### Fase 4 — Deployment Windows Server 2022 (zero-dependency, tanpa NSSM)
Satu binary `infracap.exe` dengan dua mode (native Windows Service via `golang.org/x/sys/windows/svc` + `mgr`, ter-embed saat compile — bukan dependency runtime):
- **Mode service** (default saat diluncurkan Windows SCM): implementasi `svc.Handler`, Execute loop start/stop/pause
- **Mode CLI** (manual dari CMD): subcommand `install` / `remove` / `start` / `stop` / `run` (foreground untuk debug)
- File service pakai build tag: `svc_windows.go` (`//go:build windows`) + stub `svc_other.go` (`//go:build !windows`) agar kode sama tetap jalan di Debian dev
- Install: `infracap.exe install` dari CMD admin → buat service entry via `mgr.CreateService` (startup type Automatic)
- Build statis: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o infracap.exe ./cmd/server`
- go-mssqldb pure Go → tidak butuh ODBC/Native Client di server. Deploy = copy exe + set env config → `install` → done
- Migration DB otomatis saat start pertama
- Smoke-test checklist: login, CRUD license, export Excel; stop/start service dari services.msc
- Best practice (approved): CSRF double-submit middleware, security headers + cookie flags (`HttpOnly; Secure; SameSite=Lax`), graceful shutdown, request logging ringan, `INFRACAP_AUTO_MIGRATE` env (true dev / false prod), `updated_at` diset eksplisit di query UPDATE. Rate-limit login: TIDAK diperlukan.

## 3. Testing & Validation
- Unit handler tests via `httptest` (auth guard, CSRF, validasi form, soft delete)
- Integration query ke DB dev `INFRA_CAP_DEV`
- Review manual tiap halaman di browser sebelum lanjut (review-per-page)
- **Full dev & test di Debian** — semua fitur (termasuk mode CLI service) bisa diuji lokal; hanya `svc.Handler` asli yang butuh Windows (stub di Linux mengembalikan mode foreground biasa)

## 4. Risks & Tradeoffs
1. Query manual tanpa ORM → butuh disiplin helper scan rows (satu file `queries.go` per modul).
2. DaisyUI dipilih (bukan Preline) karena class-based dan tidak butuh JS re-init saat HTMX swap.
3. Keputusan: `license_key` plaintext tanpa masking (internal app, keputusan Hiro).

## 5. Open Questions
(none — license_key ditampilkan penuh tanpa masking, keputusan Hiro)
