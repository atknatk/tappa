# M0 — Bootstrap

**Amaç.** Repodaki iskeleti gerçekten çalışır hale getirmek: ortam değişkenleri,
bağımlılıklar, ayakta bir Postgres, ilk commit ve CI. Ürün kodu yazılmaz.

**Bittiğinde:** `make check` yeşil, `make up` ile veritabanı ayakta, repoda
commit geçmişi var, CI her push'ta aynı kontrolleri koşuyor.

---

## M0-01 — .env ve kriptografik anahtarlar

- **Bağımlılık:** yok
- **Kırmızı çizgi:** §4.7 (anahtarlar repoda yer almaz)
- **Commit:** yok (`.env` gitignore'da)

**Amaç.** Uygulamanın açılabilmesi için gereken ortamı kurmak.

**Neden.** [internal/config/config.go](../../internal/config/config.go) eksik
zorunlu değerde **başlangıçta hata verir** — bu bilinçli. `.env` olmadan hiçbir
şey çalışmaz.

**Ön koşullar.** `openssl` mevcut.

**Adımlar.**
1. `cp .env.example .env`
2. İki 32 byte anahtar üret ve `.env`'e yaz:
   `openssl rand -base64 32` → `TAPPA_SESSION_HMAC_KEY`
   `openssl rand -base64 32` → `TAPPA_TAG_KEK`
3. `.env`'in git tarafından görülmediğini doğrula: `git check-ignore -v .env`

**Kabul kriterleri.**
- `.env` var, iki anahtar dolu, base64 çözümü tam 32 byte.
- `git status` çıktısında `.env` **yok**.
- Ortam yüklenmiş halde uygulama config hatası vermeden açılıyor — `make dev`
  ya da doğrudan:
  ```bash
  set -a; . ./.env; set +a; go run ./cmd/tappa   # -> level=INFO msg=listening
  ```
  Beklenen: `config:` ile başlayan hata **yok**, `msg=listening` satırı **var**.
  (DB'ye bağlanmayı henüz denemiyor; Docker kapalıyken de geçer.)
  Doğrulama bittiğinde süreç öldürülür ve **port 8080 boş bırakılır**.

> **Kart düzeltmesi (2026-07-24, M0-01 uygulaması sırasında).** Bu kriter önce
> "`go run ./cmd/tappa` config hatası vermiyor" yazıyordu; öyle **ulaşılamaz**.
> [internal/config/config.go](../../internal/config/config.go) yalnızca
> `os.Getenv` okur, Go tarafında `.env` yükleyici **yoktur**; ortamı
> [Makefile](../../Makefile) satır 22-23 (`-include .env` + `export`) sağlar.
> Çıplak `go run ./cmd/tappa` bu yüzden `config: DATABASE_URL is required …`
> verir ve bu **beklenen, doğru** davranıştır (§4: eksik zorunlu değer =
> başlangıçta hata). Kriter, gerçekte doğrulanması gereken şeyi söyleyecek
> biçimde yeniden yazıldı.

**Tuzaklar.**
- `.env.example`'daki `DATABASE_URL` ve `DATABASE_MIGRATE_URL` **farklı roller**
  kullanır; ikisini eşitleme — config bunu bilerek reddeder (RLS, tablo sahibi
  ve BYPASSRLS rolleri için atlanır).
- Anahtarları sohbete, log'a veya commit mesajına yapıştırma. Doğrulama değeri
  göstermeden yapılır (base64 çöz → **byte sayısını** bas).
- **Ortam yüklemeden `go run` çalıştırıp "bozuk" sanma** — üstteki kart
  düzeltmesine bak. `make dev` bunu senin için yapar.
- **Öksüz süreç bırakma.** `go run` bir sarmalayıcıdır: ikiliyi build cache'ten
  (`~/Library/Caches/go-build/<hash>-d/tappa`) **çocuk süreç** olarak çalıştırır.
  Yalnız `go run` PID'ini öldürürsen çocuk PID 1'e evlat edinilir ve `:8080`'i
  tutmaya devam eder → sonraki `make dev` `bind: address already in use` verir.
  Süreci **grup olarak** öldür (`set -m` + `kill -- -$PID`) ve sonucu **portla**
  doğrula (`lsof -nP -iTCP:8080 -sTCP:LISTEN` boş olmalı); süreç adı desenine
  güvenme — yolda `exe/` geçmez, yanlış desen sessizce "temiz" der.

---

## M0-02 — Go bağımlılıkları

- **Bağımlılık:** yok
- **Araç:** —
- **Commit:** `chore(deps): add pgx, uuid and templ runtime`

**Amaç.** [sqlc.yaml](../../sqlc.yaml) ve stack kararının beklediği modülleri
`go.mod`'a eklemek.

**Neden.** Şu an `go.mod`'da yalnız chi var; sqlc üretimi `pgx/v5` ve
`google/uuid` tiplerine, templ çıktısı da templ runtime'ına referans verecek.
Bunlar M1 başlamadan hazır olmalı.

**Adımlar.**
1. `go get github.com/jackc/pgx/v5`
2. `go get github.com/google/uuid`
3. `go get github.com/a-h/templ` (runtime; CLI zaten Makefile'da pinli `go run`)
4. `go mod tidy`

**Kabul kriterleri.**
- `go build ./...` temiz.
- `go.mod`'da yalnız bu dört doğrudan bağımlılık: chi, pgx/v5, uuid, templ.
- Node artefaktı yok (`make audit` içindeki N1 kontrolü temiz).

**Tuzaklar.**
- Listede olmayan bir modül eklemeden **önce sor** (CLAUDE.md §1). Özellikle:
  ORM, web framework, genel amaçlı kripto kütüphanesi, logging framework'ü.
- templ ve goose/sqlc **CLI** sürümleri Makefile'da pinli; `go get` ile ikinci bir
  sürüm getirme.

---

## M0-03 — Postgres ve rol ayrımı doğrulaması

- **Bağımlılık:** M0-01
- **Kırmızı çizgi:** §4.5 (tenant izolasyonu — RLS'in ön şartı)
- **Commit:** yok (doğrulama görevi)

**Amaç.** Veritabanının ayağa kalktığını **ve** `tappa_app` rolünün RLS'i
atlayamayacağını kanıtlamak.

**Neden.** [scripts/db-init/01-roles.sql](../../scripts/db-init/01-roles.sql)
yalnızca **ilk kez** oluşturulan volume'de çalışır. Sessizce çalışmamışsa RLS
kâğıt üzerinde kalır ve bunu M1'de değil, üretimde fark ederiz.

**Ön koşullar.** Docker daemon çalışıyor (kullanıcı başlatır).

**Adımlar.**
1. `make up`
2. Rolleri doğrula:
   ```sql
   SELECT rolname, rolsuper, rolbypassrls FROM pg_roles
   WHERE rolname IN ('tappa_owner','tappa_app');
   ```
3. `tappa_app` ile bağlanılabildiğini doğrula (`DATABASE_URL`).
4. Uzantıların yüklendiğini doğrula: `\dx` → `pgcrypto`, `citext`.

**Kabul kriterleri.**
- `tappa_app`: `rolsuper = false`, `rolbypassrls = false`.
- `tappa_owner` ve `tappa_app` **farklı** rollerdir ve ikisiyle de bağlanılabiliyor.
- `pgcrypto` ve `citext` yüklü (`gen_random_uuid()` çalışıyor).

**Tuzaklar.**
- Volume daha önce oluşmuşsa init script'i **çalışmaz**. Roller yoksa:
  `docker compose down -v` (⚠️ veriyi siler) sonra `make up`. Bu komut
  `.claude/settings.json`'da `deny` listesinde — kullanıcıdan izin iste.
- `make down` volume'ü silmez; bu doğru davranış.

---

## M0-04 — Üretim hattı doğrulaması (templ · sqlc · tailwind)

- **Bağımlılık:** M0-02
- **Commit:** yok (doğrulama görevi)

**Amaç.** `make gen` ve `make css`'in gerçekten çalıştığını, ilk gerçek kaynak
dosya yazılmadan önce görmek.

**Neden.** Bu hatlar boş girdiyle de çalışmalı. M1'in ortasında "sqlc kurulmuyor"
ile uğraşmak, işi iki kez böler.

**Adımlar.**
1. `make tools` → `.tools/tailwindcss` iner (varsa atlar).
2. `make css` → `web/static/css/app.css` üretilir.
3. `make templ` → `.templ` dosyası yok, hatasız geçmeli.
4. `make sqlc` → şema ve sorgu boş; hata veriyorsa mesajı not et
   (sqlc boş şemayla "no queries" diyebilir — bu kabul edilebilir, M1-08'de dolacak).

**Kabul kriterleri.**
- `make css` çıktı üretiyor, `app.css` gitignore'da (üretilen dosya).
- `make templ` sıfır hata.
- `make sqlc` ya temiz geçiyor ya da yalnızca "girdi yok" anlamında uyarıyor;
  kurulum/sürüm hatası **yok**.

**Tuzaklar.**
- Makefile `.ONESHELL` kullanmıyor (macOS'un GNU Make 3.81'i yüzünden) — çok
  adımlı iş eklemen gerekirse `scripts/` altına script yaz, Makefile'a satır yığma.
- `app.css` gitignore'da ama `*_templ.go` ve `internal/store/*.go` **commit edilir**.

---

## M0-05 — İlk commit ve dal stratejisi

- **Bağımlılık:** M0-01 … M0-04
- **Commit:** `chore: scaffold repo, tooling and working plan`

**Amaç.** Repoya ilk commit'i atmak ve bundan sonraki dal düzenini kurmak.

**Neden.** Repoda **hiç commit yok**; her şey staged durumda. Bu yüzden
`make check` içindeki `git diff --exit-code` şu an anlamsız çalışıyor, ayrıca
"kaldığımız yer" bir commit hash'ine bağlanamıyor — `state.md`'nin ledger'ı buna
dayanıyor.

**Adımlar.**
1. `git status` ile staged içeriği gözden geçir; `.env`, `bin/`, `.tools/`,
   `app.css` **girmemeli**.
2. Bu plan klasörünü (`docs/plan/`) ve varsa CLAUDE.md güncellemesini ekle.
3. İlk commit `main` üzerine atılır (dal açılacak taban yok).
4. Bundan **sonraki her iş için dal**: `git switch -c m1-schema` gibi;
   `main`'e doğrudan commit yok (CLAUDE.md §10).
5. Kullanıcı istemedikçe push/PR **yok**.

**Kabul kriterleri.**
- `git log --oneline` en az bir commit gösteriyor.
- `git status` temiz.
- Sırlar ve üretilen artefaktlar commit'te yok.

**Tuzaklar.**
- Commit mesajı İngilizce ve emir kipi (`add`, `fix`, değil `added`).
- `git commit` `.claude/settings.json`'da `ask` listesinde — onay isteyecek.

---

## M0-06 — CI iş akışı

- **Bağımlılık:** M0-05
- **Commit:** `ci: run make check and audit on push`

**Amaç.** `make check` ve `make audit`'i her push'ta koşan bir GitHub Actions
iş akışı eklemek.

**Neden.** [CLAUDE.md](../../CLAUDE.md) §2 `make check`'i "CI'nin çalıştırdığı"
diye tanımlıyor ama **CI dosyası yok**. Kural mekanik olarak zorlanmıyorsa er
geç unutulur.

**Ön koşullar.** Q04 (DB testleri neye karşı koşacak) cevaplanmış olmalı —
CI'da Postgres servisi gerekip gerekmediğini belirler.

**Dokunulacak dosyalar.** `.github/workflows/ci.yml`

**Adımlar.**
1. Go 1.26 kurulumu + modül önbelleği.
2. `make tools` (tailwind ikilisi indirilir — sürüm pinli).
3. `make check` (fmt + lint + test + üretilen dosya kontrolü).
4. `make audit` (govulncheck + `scripts/redline-check.sh`; `ripgrep` kurulmalı,
   yoksa script taramayı sessizce atlar).
5. Q04 = yerel Postgres ise `services: postgres:17` ve init SQL'in uygulanması.

**Kabul kriterleri.**
- İş akışı push ve PR'da tetikleniyor.
- `make check` ve `make audit` CI'da yeşil.
- `rg` CI'da kurulu — aksi halde redline taraması sessizce atlanır ve yanlış
  güven verir.

**Tuzaklar.**
- `make check` sonunda `git diff --exit-code` var: `make gen`/`make fmt`
  çıktısı commit edilmemişse CI kırmızı olur. Bu **istenen** davranış.
- CI'da `CGO_ENABLED=0` ve `TZ=UTC` ayarlı olsun (repo settings.json'daki
  yerel ortamla eşleşsin).
