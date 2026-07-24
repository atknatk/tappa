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
3. `go get github.com/a-h/templ@v0.3.833` (runtime; sürüm
   [Makefile](../../Makefile) satır 16'daki `TEMPL_VERSION` ile **aynı** olmalı —
   CLI `go run ... @sürüm` ile pinli, ikinci bir kopya `go.mod`'a girmez)

> ⚠️ **`go mod tidy` çalıştırma.** M1'de bu modülleri import eden ilk Go dosyası
> yazılana kadar `tidy` onları `go.mod`'dan **düşürür** (import edilmeyen
> requirement = kullanılmayan requirement). Yanlışlıkla çalıştırdıysan zararı
> kalıcı değil: yukarıdaki üç `go get`'i tekrarla, durum geri gelir. İlk gerçek
> import geldikten sonra `tidy` normal davranışına döner ve serbesttir.

**Kabul kriterleri.**
- `go build ./...` temiz.
- **Dışlayıcı liste.** `go.mod`'da bu dört modül — chi, pgx/v5, uuid, templ — ve
  **yalnızca** pgx'in kendi zinciri (`pgpassfile`, `pgservicefile`,
  `golang.org/x/text`) bulunur; **başka hiçbir modül yok**. Dördünün sürümü
  `go.sum` ile sabitlenmiş. **`direct` işareti M1'deki ilk import ile oluşur** —
  o güne kadar üçü `// indirect` görünür (bkz. kart düzeltmesi).
  Mekanik doğrulama:
  ```bash
  go mod edit -json | jq -r '.Require[].Path' | sort
  ```
  **M0-02 anında çıktı tam olarak şu 7 satırdır** — bu bir referans değer,
  değişmez bir sayı değil: `github.com/a-h/templ` · `github.com/go-chi/chi/v5` ·
  `github.com/google/uuid` · `github.com/jackc/pgpassfile` ·
  `github.com/jackc/pgservicefile` · `github.com/jackc/pgx/v5` ·
  `golang.org/x/text`.
  **M1'de sayı 9'a çıkar ve bu beklenendir:** [M1-07](m1-veri-katmani.md)
  `pgxpool`'u import ettiği anda pgx zinciri `github.com/jackc/puddle/v2` ve
  `golang.org/x/sync` ile büyür. İkisi de pgx'in kendi bağımlılığıdır, yeni bir
  bağımlılık **kararı** değil — "dur ve sor" bunlar için tetiklenmez.
  Kural değişmiyor: **bu dokuz adın dışında bir modül = onaysız bağımlılık** →
  kriter **düşer**, dur ve sor (CLAUDE.md §1).
- templ runtime sürümü `Makefile`'daki `TEMPL_VERSION` ile aynı olmalı; ikisini
  birlikte yükseltmek ayrı ve bilinçli bir commit'tir ([Makefile](../../Makefile)
  satır 12).
- Node artefaktı yok — N1 kontrolü temiz. **Doğrulama komutu doğrudan
  `./scripts/redline-check.sh`'tir**, `make audit` değil: audit hedefi
  `govulncheck` → `redline-check.sh` sırasıyla koşar ([Makefile](../../Makefile)
  satır 122-124) ve govulncheck bugün exit 3 verdiği için make önce durur, N1'e
  **hiç ulaşmaz**. **M0-07** audit'i yeşile alana kadar N1 doğrudan script ile
  doğrulanır.

> **Kart düzeltmesi (2026-07-24, M0-02 uygulaması sırasında).** İki madde
> gerçeği yansıtmıyordu.
>
> **1) Adım 4 (`go mod tidy`) kendi adım 1-3'ünü siliyordu.** `tidy`, main
> module'deki hiçbir paketin import etmediği requirement'ları kaldırır. Bu
> repoda `pgx`, `uuid` ve `templ`'i import eden bir Go dosyası **henüz yok**
> (sqlc çıktısı M1-08'de, templ çıktısı M1'de doğar), dolayısıyla `tidy` üçünü
> de `go.mod` ve `go.sum`'dan düşürüyordu — `go mod tidy -diff` ile doğrulandı.
> Adım listeden çıkarıldı, yerine uyarı bloğu kondu.
>
> **2) "Yalnız dört *doğrudan* bağımlılık" kriteri ulaşılamazdı.** Go, import
> edilmeyen bir modülü `go get` ile eklerken zorunlu olarak `// indirect`
> işaretler; `direct` işareti ancak bir paket onu import ettiğinde oluşur. Bunu
> bugün zorlamanın tek yolu geçici bir `//go:build tools` blank-import dosyası
> yazmaktı; M1'deki ilk gerçek import ile silinecek bir iskele olduğu için
> **eklenmedi** (CLAUDE.md §1). Kriter, gerçekte doğrulanabilir olanı söyleyecek
> biçimde yeniden yazıldı.
>
> Eski kriter iki şey söylüyordu: (a) dördü `direct` olacak, (b) **başkası
> olmayacak**. Yalnız (a) ulaşılamazdı; **(b) dışlayıcılık aynen korundu** ve
> mekanik doğrulaması eklendi. Bu bilerek yapıldı: aksi hâlde `go.mod`'a onaysız
> bir beşinci modül (ORM, logging framework, kripto kütüphanesi — CLAUDE.md §1'in
> özellikle saydıkları) girse M0-02 hâlâ "geçti" derdi.
>
> Dikkat: sabit olan **sayı değil, listedir**. 7 satır yalnızca M0-02 anının
> referansıdır; M1-07 `pgxpool`'u import edince `puddle/v2` + `golang.org/x/sync`
> eklenir ve 9 olur. Bu bir ihlal değil, pgx zincirinin normal büyümesidir —
> kriteri "zaten tutmuyor" diye göz ardı etme, dokuzluk listeye göre uygula.
>
> Ayrıca adım 3'e sürüm pini eklendi: sürümsüz `go get` runtime'ı `v0.3.1020`
> getiriyordu, `Makefile`'daki CLI ise `v0.3.833`. Üretilen `*_templ.go` kodu
> runtime API'sini çağırdığı için bu ayrışma sessiz uyumsuzluk riskidir.

**Tuzaklar.**
- Listede olmayan bir modül eklemeden **önce sor** (CLAUDE.md §1). Özellikle:
  ORM, web framework, genel amaçlı kripto kütüphanesi, logging framework'ü.
- templ ve goose/sqlc **CLI** sürümleri Makefile'da pinli; `go get` ile ikinci bir
  sürüm getirme.
- `go get` ile eklenen ama henüz import edilmeyen modül `go.mod`'da `// indirect`
  görünür. Bu **beklenen** davranıştır, bozukluk değil — düzeltmeye çalışma.
- `pgx` yanında `pgpassfile`, `pgservicefile` ve `golang.org/x/text` de gelir.
  Bunlar pgx'in kendi zinciri, yeni bir bağımlılık **kararı** değil.

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

- **Bağımlılık:** M0-05, **M0-07** (`make check` ve `make audit` bugün kırmızı;
  yeşile alınmadan CI kurmak yalnızca kırmızı bir rozet üretir)
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

---

## M0-07 — `make check` ve `make audit`'i yeşile alma

- **Bağımlılık:** M0-02 · [Q26](open-questions.md)
- **Kırmızı çizgi:** §4.5 (IP, *proof of place*'in 50 puanlık ayağı — sahtelenebilir
  IP, sahte kanıttır)
- **Commit:** `chore: make check and audit pass`

**Amaç.** CLAUDE.md §2'nin *"CI'nin çalıştırdığı"* dediği iki hedefi gerçekten
yeşile almak.

**Neden.** İkisi de bugün **kırmızı**; bu hâlleriyle [M0-06](#m0-06--ci-iş-akışı)
yalnızca kırmızı bir rozet üretir. Bulgular M0-02 denetimi sırasında çıktı ama
**M0-02'nin sebep olduğu şeyler değil** — biri ilk commit'ten beri duran kod,
diğeri yerel toolchain sürümü. Ayrı kart olmalarının sebebi bu.

### Bulgu 1 — `make check` kırmızı: staticcheck SA1019

```
internal/httpx/router.go:23:8: middleware.RealIP is deprecated: RealIP is
vulnerable to IP spoofing — it mutates r.RemoteAddr to the leftmost
X-Forwarded-For value, or to True-Client-IP / X-Real-IP whether or not your
infrastructure actually sets them. See GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp,
GHSA-9g5q-2w5x-hmxf. (SA1019)
```

[router.go:20-22](../../internal/httpx/router.go)'deki uyarı yorumu (P7'de
düzeltilmişti) doğru ama **yetersiz**: yorum ne linter'ı susturur ne riski kaldırır.

**Seçenekler.**

| | Ne | Artı | Eksi |
|---|---|---|---|
| **(a)** | `middleware.RealIP`'i router'dan **tamamen çıkar** | Bugün davranış değişmez: router'ın tek tüketicisi `/healthz` ve `/static`, ikisi de istemci IP'si okumaz. Sahte *proof of place* taşımaktansa hiç taşımamak dürüst. M5-03 doğrusunu getirecek. | Yok denecek kadar az; M5-03'e kadar `r.RemoteAddr` ham TCP eşi kalır — ki **doğrusu** budur |
| **(b)** | Gerekçeli `//lint:ignore SA1019` | Tek satır | Ölü ama tehlikeli kodu ayakta tutar; susturma M5-03'ten sonra da sağ kalma eğilimindedir; tam da susturulmaması gereken sınıf bir güvenlik uyarısını susturur |
| **(c)** | [M5-03](m5-tap-akisi.md)'ü öne al | Kalıcı doğru çözüm | `cfg.TrustedProxies` + CIDR ayrıştırma + testler = M0'a M5 işi sokmak. M0'ın tanımı "ürün kodu yazılmaz" |

**Karar: (a) — RealIP router'dan çıkarılır.** Gerekçe: §5 satır 6'da IP eşleşmesi
**50 güven puanı** taşıyor, tek başına en ağır kanıt. Bu ağırlığın altına M4/M5'e
kadar sahtelenebilir bir değer koymak, ilerideki bir hatanın sessizce güvenlik
sınırını taşımasına zemin hazırlar. Bugün hiçbir tüketicisi olmadığına göre
maliyeti sıfır. Bu bir mimari kararın geri alınması **değil** — gerçek karar
zaten M5-03'ün; o yüzden **ADR gerektirmiyor**, gerekçe bu kartta yazılı olması
yeterli. (Sonuçta (b) veya (c) seçilirse durum değişir: ikisi de güvenlik
sınırına dokunur → CLAUDE.md §10 gereği **ADR yazılır**.)

### Bulgu 2 — `make audit` kırmızı: govulncheck

`govulncheck` **exit 3**; dördü de çağrı yolunda ve **tamamı `stdlib@go1.26.2`**:

| ID | Paket | Düzeltildiği sürüm |
|---|---|---|
| `GO-2026-5856` | `crypto/tls` (ECH gizlilik sızıntısı) | go1.26.**5** |
| `GO-2026-5039` | `net/textproto` (hata mesajında kaçışsız girdi) | go1.26.4 |
| `GO-2026-5037` | `crypto/x509` (verimsiz hostname ayrıştırma) | go1.26.4 |
| `GO-2026-4971` | `net` (Windows'ta NUL byte panic) | go1.26.3 |

**Hiçbiri pgx / uuid / templ / chi kaynaklı değil** — yani M0-02 ile ilgisi yok.
Tarama ayrıca import edilen paketlerde 4, gerekli modüllerde 5 açık daha buluyor
ama **çağrı yolunda değiller**. Çözüm tek: toolchain'i **≥ go1.26.5**'e yükseltmek
— bu bir **kullanıcı eylemi**, [Q26](open-questions.md) ile takip ediliyor.

**Ön koşullar.** Q26 cevaplanmış ve toolchain yükseltilmiş olmalı; aksi hâlde bu
kartın ikinci kabul kriteri karşılanamaz.

**Dokunulacak dosyalar.** `internal/httpx/router.go` (yalnız Bulgu 1).

**Adımlar.**
1. `r.Use(middleware.RealIP)` satırını sil.
2. [router.go:20-22](../../internal/httpx/router.go)'deki yorumu **güncelle**,
   silme: artık var olmayan bir middleware'i anlatmamalı, ama M5-03'e giden
   işaretçiyi korumalı (ör. *"istemci IP'sine güvenilmiyor; güvenilir-proxy
   sınırlı gerçek çözüm M5-03'te"*).
3. `middleware` import'u **kalır** — `RequestID`, `Recoverer`, `Timeout` hâlâ kullanıyor.
4. Toolchain'i Q26 kararına göre yükselt (kullanıcı eylemi).
5. `make check` ve `make audit`.

**Kabul kriterleri.**
- `make check` **yeşil** (fmt + vet + staticcheck + test + `git diff --exit-code`).
- `make audit` **yeşil** — `govulncheck` exit 0 **ve** `redline-check.sh` temiz.
- RealIP kararı gerekçesiyle yazılı: ADR gerekmiyorsa bu kartta (yukarıda),
  (b)/(c) seçildiyse `docs/adr/NNNN-*.md`'de.
- `./scripts/redline-check.sh` hâlâ temiz (RealIP'in kaldırılması hiçbir R kuralını
  tetiklememeli).

**Tuzaklar.**
- **`make audit` sırayla çalışır: `govulncheck` → `redline-check.sh`.** govulncheck
  kırmızıyken make durur ve **kırmızı çizgi taraması hiç koşmaz**. Bugünkü durum
  budur; "audit kırmızı" = "redline denetlendi ve kaldı" **değil**, "redline hiç
  denetlenmedi" demektir.
- **Adım 1, govulncheck çıktısını değiştirmez** — Bulgu 1 ile Bulgu 2 bağımsızdır.
  `GO-2026-5856`'nın izlerinden biri `RealIP` **çağrısından** değil, chi
  `middleware` **paketinin import'undan** geçiyor (`router.go:10:2: httpx.init
  calls middleware.init`); birinci izi ise doğrudan `ListenAndServe`'den geliyor.
  Yani RealIP kaldırıldıktan sonra da dört açık aynen durur; Bulgu 2 **salt
  toolchain sürümüne** bağlıdır. Adım 1'i uygulayıp audit hâlâ kırmızı diye
  "işe yaramadı" sanma.
- `govulncheck@latest` pinli değil ([Q25](open-questions.md) b maddesi): kod
  değişmeden tarama sonucu değişebilir, CI bir sabah kendiliğinden kırmızıya döner.
  Q25 bu kartla birlikte kapatılırsa iyi olur.
- (b) seçilirse: staticcheck **kullanılmayan** `//lint:ignore` direktifini de hata
  sayar — M5-03 RealIP'i kaldırdığında bayat direktif kendisi lint hatası olur.
- Toolchain yükseltmesi `go.mod`'daki `go 1.26.2` satırına dokunmayı gerektirmez;
  o satır **asgari** sürümdür, yüklü toolchain'den bağımsızdır. Gereksiz yere
  değiştirme.
