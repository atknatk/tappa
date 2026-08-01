# Tappa — Çalışma Kuralları

> **Tappa** — "Punchless time & attendance." NFC tabanlı, cihazsız mesai takip SaaS'ı.
> NFC etiketi çalışanın cebinde değil **duvarda**: her lokasyon girişine pasif bir
> plaket (NTAG 424 DNA) monte edilir. Çalışan kendi telefonunu değdirir → tarayıcı
> açılır (uygulama kurulumu YOK) → kalıcı oturumdan tanınır → tek butonla check-in/out.
>
> Ürün/pazar/marka detayı: **[docs/handoff.md](docs/handoff.md)** — kararların gerekçesi orada.
> Bu dosya *nasıl çalışacağımızı* anlatır, *ne inşa ettiğimizi* değil.

---

## 1. Stack — sabit, tartışmaya kapalı

| Katman | Seçim | Gerekçe |
|---|---|---|
| Dil | **Go 1.26** | tek binary, statik deploy |
| Router | **chi** (stdlib `net/http` üstüne) | framework kilidi yok, `http.Handler` uyumlu |
| DB sürücü | **pgx/v5** (pool) | Postgres'e en yakın, `database/sql` katmanı yok |
| Sorgu | **sqlc** — SQL yazılır, Go üretilir | ORM YOK; RLS ve atomik `ctr` update'i birebir kontrol |
| Migration | **goose** (`db/migrations/*.sql`) | düz SQL, geri alınabilir |
| HTML | **templ** (tip güvenli) + **HTMX** | tek binary, SPA yok |
| CSS | **Tailwind standalone CLI** | **Node YOK** — `.tools/tailwindcss` ikilisi |
| Deploy | tek VPS (AB bölgesi) + managed Postgres | mikroservis YOK |

**Node/npm/node_modules bu repoda ASLA yer almaz.** Bir çözüm Node gerektiriyorsa, o
çözüm yanlış çözümdür — alternatif ara veya sor.

Yeni bağımlılık eklemeden önce sor. `stdlib > küçük kütüphane > framework`.

## 2. Komutlar

```bash
make up            # postgres ayağa kalkar
make migrate seed  # şema + KF/KM demo verisi
make dev           # gen + css + çalıştır → localhost:8080
make gen           # templ + sqlc üretimi   ← .templ veya .sql değişince ZORUNLU
make test          # go test -race ./...
make check         # fmt + lint + test (CI'nin çalıştırdığı)
make audit         # govulncheck + kırmızı çizgi taraması
```

`.templ` veya `db/queries/*.sql` düzenledikten sonra `make gen` çalıştırmadan
"derlemiyor" deme. Üretilen dosyalar (`*_templ.go`, `internal/store/*.go`)
**elle düzenlenmez**, ama commit **edilir**.

## 3. Dizin haritası — nereye ne yazılır

```
cmd/tappa/            main; wiring ve graceful shutdown. İş mantığı YOK.
internal/config/      env okuma + doğrulama. Eksik zorunlu env = başlangıçta panic.
internal/httpx/       router, middleware (request id, tenant, auth, real-ip, rate limit)
internal/handler/     HTTP handler + DTO. Kural: parse → domain çağır → render.
                      Handler içinde iş kuralı veya SQL YOK.
internal/policy/      ⭐ policy motoru. Guardrail (kapatılamaz) + baseline +
                      tenant politikaları; effect döndürür, kayıt yazmaz.
                      Saf fonksiyon. Detay: docs/plan/m3-policy-motoru.md
internal/domain/tap/  ⭐ tap karar motoru. §5 buranın spec'i. Saf fonksiyon,
                      DB/HTTP bilmez → tablo bazlı testlerle kanıtlanır.
internal/domain/tenant/  tenant/lokasyon/departman/çalışan iş kuralları
internal/sun/         NTAG 424 DNA SDM doğrulama: AES-CMAC + ctr. Kriptografi
                      SADECE burada. Skill: `tappa-sun`
internal/session/     oturum token üretimi/doğrulama (hash saklanır, token değil)
internal/store/       sqlc ÜRETİMİ — elle dokunma
internal/geo/         haversine, GPS yarıçap kontrolü
db/migrations/        goose SQL. Uygulanmış migration DEĞİŞTİRİLMEZ, yenisi yazılır.
db/queries/           sqlc kaynak SQL
web/templates/        .templ dosyaları
web/static/           css/img/js — Go binary'sine embed edilir
docs/adr/             mimari kararlar (NNNN-baslik.md)
test/fixtures/        seed.sql + test vektörleri
```

## 4. Kırmızı çizgiler — ihlal edilemez

Bunlar tercih değil, ürünün varlık sebebi. Bir görev bunlardan birini ihlal
ediyorsa **dur ve sor**, sessizce uygulama.

1. **Biyometrik veri asla toplanmaz/saklanmaz.** Parmak izi, yüz, ses — hiçbiri.
   Telefonun kendi ekran kilidi bizim katmanımız değildir; ondan veri almayız.
2. **GPS yalnızca tap anında okunur.** Arka plan konum, sürekli takip, geofence
   izleme YOK. Bu hem GDPR gereği hem satış argümanı.
3. **`transactions` immutable.** UPDATE/DELETE yok. Düzeltme = yeni kayıt +
   `audit_log`. Mesai kaydı hukuki delil olabilir.
4. **`ctr` kontrolü atomik.** Oku-sonra-yaz replay açığıdır. Tek ifadede:
   `UPDATE tags SET last_ctr=$2 WHERE uid=$1 AND last_ctr < $2 RETURNING …`
   Etkilenen satır 0 ise → REJECT. Bunu asla transaction dışı iki adıma bölme.
5. **Tenant izolasyonu her katmanda.** Her tabloda `tenant_id`, her tabloda RLS
   politikası, uygulama `tappa_app` rolüyle bağlanır (NOBYPASSRLS, tablo sahibi
   değil). Sorgularda ayrıca açık `tenant_id` filtresi — kuşak+kemer.
6. **Kayıt asla kaybolmaz.** Kanıt yetersizse REJECT edip atma: kaydı yaz,
   `verdict='flag'` ver, müdür onay kuyruğuna düşür. Sessiz onay da yok.
7. **NTAG AES anahtarları repoda yer almaz.** DB'de KEK ile sarmalanmış saklanır
   (`tags.aes_key_ref`), log'a asla yazılmaz. Test vektörleri ayrı ve sahte.

`make audit` bu maddeleri mekanik olarak tarar; geçmesi ihlal olmadığını kanıtlamaz.

## 5. Tap karar motoru — spec

Bu bölüm `internal/domain/tap` **ve** `internal/policy` baseline'ı için normatif
metindir. Kod bundan sapıyorsa kod yanlıştır.

Kararlar kod içi `if` zinciri değil, **policy motorundan** gelir:
**satır 1–5 guardrail** (`sys:*`, kodda gömülü, hiçbir müşteri kapatamaz) ·
**satır 6–7 baseline** (`base:*`, tenant değiştirebilir). Hiçbir politika
eşleşmezse sonuç `flag`'dir — sessiz onay imkânsız (§4.6). `tap.Decide` bağlamı
kurar ve dönen effect'i uygular; yön, trust ve geç kalma **hesaptır**, politika
değildir.

**Dört kanıt:** SUN = *şu an fiziksel dokunuş* · oturum çerezi = *kim* ·
statik IP = *nerede* · GPS = *yedek nerede*.

**Karar sırası — ilk eşleşen kazanır:**

| # | Koşul | Sonuç |
|---|---|---|
| 1 | Etiket `lost` veya `retired` | `reject` |
| 2 | SUN geçersiz (CMAC uyuşmuyor **veya** `ctr` artmadı) | `reject` |
| 3 | Oturum yok / geçersiz | aktivasyon sayfası (kayıt yok) |
| 4 | Çalışan `deactivated` | `reject` + denemeyi logla + güvenlik uyarısı |
| 5 | Aynı **kişi** 60 sn içinde tekrar | `ignored` |
| 6 | IP eşleşti **veya** GPS < 150 m | `ok` (IP yoksa not: `verified via GPS`) |
| 7 | ikisi de yok | `flag` — kayıt alınır, onay kuyruğuna düşer |

- **Debounce KİŞİ bazlıdır, etiket bazlı değil.** Sıra halinde farklı kişiler
  aynı plakete art arda dokunabilmeli.
- **Debounce'un "60 sn"si SUNUCU zamanıdır** (ADR 0006, 2026-08-01). Mesafe iki
  ölçünün **küçüğüdür**: istemcinin beyan ettiği `occurred_at` farkı **ve**
  veritabanının hesapladığı kayıt yaşı (`clock_timestamp() − created_at`, yalnız
  tap kanalları; `manual` öncül muaf — müdürün yazdığı satır dokunuş değildir).
  Yalnız beyana bakmak dört ayrı şekilde aşılabiliyordu (ölçüldü): geriye tarihleme ·
  öncülü seçme (`ORDER BY occurred_at`) · ileri tarihleme (negatif mesafe guardrail'i
  kapatıyordu) · eşzamanlılık. Sonuncusu için karar + kayıt **kişi başına advisory
  kilit** altında tek transaction'da koşar. **QR'da bu tek frendir** — sayaç yok.
- **Güven puanı:** `20 (taban) + 50 (IP eşleşti) + 30 (GPS eşleşti)`.
- **Yön (in/out):** kişinin **son açık girişine** göre toggle — takvim gününe göre
  DEĞİL. Rusty Bar 18:00–02:00 vardiyasında 02:00 çıkışı doğru eşleşmeli.
- **Geç kalma** çalışanın KENDİ vardiyasına göre: departman vardiyası varsa o,
  yoksa **tap edilen** lokasyonun vardiyası (profildeki lokasyon değil — zincirde
  şube değişimi normaldir). Farklı şubede tap nota işlenir, raporda ayrı görünür.
- **Unutulan çıkış:** sistem çıkış kaydı **üretmez**. Açık kayıt anomali olarak
  listelenir, müdür manuel girer. Açık kayıtlar saat toplamına **girmez** ve
  raporda toplamın eksik olduğu açıkça söylenir.
- **QR kanalı** (`channel='qr'`): SUN yok → **IP zorunlu**, GPS tek başına yetmez
  → `flag`. QR fotoğraflanır ve süresiz geçerlidir; fiziksel dokunuş kanıtı yok.
  Baseline politikası `base:qr-requires-ip`, tenant değiştirebilir.
- **Practice tap:** aktivasyon sonrası ilk kayıt `practice=true`, TRAINING damgası,
  çalışılan saate **asla** sayılmaz.
- **Manuel kayıt:** `channel='manual'` + `entered_by` dolu; raporlarda ayrı görünür.

Bu tablodaki her satırın adı `TestDecide_...` ile başlayan bir test durumu olmalı.

## 6. Veritabanı kuralları

- Her yeni tablo şunlarla doğar: `tenant_id uuid NOT NULL`, `ENABLE ROW LEVEL SECURITY`,
  `FORCE ROW LEVEL SECURITY`, `tenant_id` üzerinde politika, `tenant_id` üzerinde indeks.
  Beşinden biri eksikse migration eksiktir (GRANT dahil; bkz. docs/plan/m1-veri-katmani.md).
- Tenant bağlamı bağlantı başına `SET LOCAL app.tenant_id` ile verilir; politikalar
  **`NULLIF(current_setting('app.tenant_id', true), '')::uuid`** okur. `SET LOCAL` →
  transaction dışında sızmaz. Havuzdan alınan bağlantıda `SET` (LOCAL'siz) **kullanma**.
  `NULLIF` şart, süs değil: GUC'a bir kez **yazıldıktan** sonra bağlantıda `NULL`'a
  dönmez, `''` kalır (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de) — çıplak cast o
  bağlantıda `NULL` değil **hata** verir, yani davranış bağlantı geçmişine bağlı olur.
  Ölçüm ve gerekçe: docs/plan/open-questions.md → Q27, ADR 0002.
- **İzolasyon testi ile üretim sorgusu farklı şekiller ister.** Üretim sorguları
  §4.5 gereği açık `tenant_id` filtresi taşımak **zorunda**; RLS izolasyon testi
  ise taşımamalı — filtre varsa 0 satırın sebebi RLS değil `WHERE`'dir ve test RLS
  kapalıyken de yeşil kalır (ölçüldü). İkisi çelişki değil, farklı iş.
- Zaman: DB'de her şey `timestamptz`, **UTC**. Yerel saate çevirme sadece render
  katmanında. Gece vardiyası bug'larının kaynağı budur.
- Para/saat hesabı `float` ile yapılmaz.
- Migration geri alınabilir olmalı (`-- +goose Down` dolu).
- Yeni sorgu = `db/queries/*.sql` + `make sqlc`. Handler içine ham SQL yazılmaz.

## 7. Go kod kuralları

- Hata sarmalama: `fmt.Errorf("checkin: %w", err)`. Sessizce yutma yok; `_ = err` yok.
- Handler'lar `context.Context` taşır ve iptal eder. Global state, `init()` sihri yok.
- Bağımlılıklar açıkça enjekte edilir (struct alanı), paket seviyesi singleton yok.
- Arayüz **tüketici** tarafında tanımlanır, üretici tarafında değil.
- Log: `log/slog`, yapılandırılmış. **Asla loglanmaz:** oturum token'ı, CMAC,
  AES anahtarı, davet kodu, tam GPS koordinatı.
- Dış girdi handler sınırında doğrulanır; domain katmanı zaten geçerli veri görür.
- Yorumlar *neden*i anlatır. Kodun ne yaptığını tekrar eden yorum yazma.
- Türkçe karakter kodda yok: identifier, log, hata mesajı, commit → İngilizce.
  Kullanıcıya görünen metinler ve bu repodaki dokümanlar Türkçe/İngilizce olabilir.

## 8. Test kuralları

- `internal/domain/tap` ve `internal/sun` için kapsam hedefi **%90+**. Bunlar ürünün
  doğruluk çekirdeği.
- Tablo bazlı testler (`tests := []struct{name string; …}`), her karar satırı için bir vaka.
- `internal/sun` testleri **bilinen cevap vektörleriyle** çalışır: geçerli imza,
  sayaç tekrarı, sayaç geriye gitmesi, yanlış anahtar, bozuk hex.
- Replay koruması için gerçek eşzamanlılık testi: aynı `(tag, ctr)` ile N goroutine →
  tam olarak **1** başarılı. `-race` ile.
- DB testleri gerçek Postgres'e karşı (testcontainers veya `make up`), sahte değil —
  RLS'i sahte veritabanı test edemez.
- **RLS testi zorunlu:** A tenant'ının bağlantısı B tenant'ının satırını görmemeli.

## 9. Arayüz & marka

Detay ve token'lar: **skill `tappa-brand`** (UI'a dokunmadan önce oku).

Özet: paletin dışına çıkma (`ink #152219`, `porcelain #EDF0EA`, `tappa-green #1F5C41`,
`saffron #D98E2B`, `tomato #BE3D2A`, `paper #FFFDF4`); Space Grotesk (display) +
IBM Plex Mono (veri). İmza motif **mutfak adisyonu (kitchen docket)** — işlem kayıtları
perforeli fiş kartı, APPROVED/FLAGGED/REJECTED hafif eğik kaşe damgası.

Çalışan tap ekranı kutsaldır: **tek ekran, tek buton, sıfır öğrenme**. Onay ekranında
buton YOK — "All done — you can close this page." Bir sonraki işlem zaten yeni fiziksel
dokunuş gerektirir. Bu ekrana özellik eklemek istiyorsan önce sor.

## 10. Çalışma şekli

- **Ana oturum iş yapmaz, organize eder.** Her görev bir alt ajana (model `opus`)
  yaptırılır; ardından **ayrı** bir üçüncü göz ajanı kabul kriterlerini denetler.
  Bulgu varsa düzelttirilip **yeniden** denetlenir. Onay gelmeden görev `done`
  olmaz ve sonrakine geçilmez. Ayrıntı:
  [docs/plan/README.md → Çalışma modu](docs/plan/README.md).
- **Oturuma [docs/plan/state.md](docs/plan/state.md) ile başla** — sıradaki görev,
  blokeler ve sağlık kontrolü orada. Protokolün tamamı: skill `tappa-next`.
- Görev başlarken ilgili bölümü oku (`docs/handoff.md` ürün, bu dosya kural,
  `docs/plan/m*-*.md` görevin spec'i).
- İş bitince **`docs/plan/state.md`'yi güncelle** (ledger + "ŞU AN" + oturum notu).
  Atlanırsa bir sonraki oturum yanlış yerden başlar.
- Küçük ve odaklı değişiklik; ilgisiz refactor'ü aynı commit'e karıştırma.
- Şema, karar motoru veya güvenlik sınırı değiştiyse **ADR yaz** (`docs/adr/NNNN-*.md`).
- İş bitince `make check` çalıştır. Kırmızıysa iş bitmemiştir.
- Commit: `type(scope): özet` — `feat(tap): reject retired tags`. İngilizce, emir kipi.
- **Doğrudan `main`'de çalışılır** (kullanıcı kararı, 2026-07-24): yeni proje,
  görev/milestone başına feature dal açılmaz. Kullanıcı istemedikçe **push/PR
  açma** — commit yerelde `main`'e atılır. (M0, `m0-bootstrap` dalında yapılıp
  `main`'e birleştirildi; sonrası doğrudan `main`.)
- Emin değilsen sor — özellikle §4 kırmızı çizgilere değen her şeyde.

## 11. Ajanlar & skill'ler

| Ne zaman | Araç |
|---|---|
| Oturum başı/sonu, "sırada ne var", "kaldığımız yerden devam" | skill `tappa-next` |
| Karar kuralı, tenant'a göre değişen davranış, "bunu müşteri ayarlasın" | `internal/policy` — [docs/plan/m3-policy-motoru.md](docs/plan/m3-policy-motoru.md) |
| Güvenlik/kırmızı çizgi denetimi (diff üstünde) | agent `tappa-security-auditor` |
| Yeni tablo/sorgu: migration + RLS + sqlc + test | agent `tappa-db-migrator` |
| Herhangi bir UI/templ/Tailwind işi | skill `tappa-brand` |
| NTAG 424 DNA / SUN / CMAC / ctr | skill `tappa-sun` |
| Demo veri, fixture, "bir günü simüle et" | skill `tappa-seed` |

## 12. Durum

**Canlı durum tek yerdedir: [docs/plan/state.md](docs/plan/state.md).** Bu dosyaya
durum yazma — çelişirler.

Plan sistemi [docs/plan/](docs/plan/) altında: [roadmap](docs/plan/roadmap.md)
(M0–M9, 81 görev, sıralama gerekçesi) · `m*-*.md` görev kartları ·
[state.md](docs/plan/state.md) (nerede kaldık) ·
[open-questions.md](docs/plan/open-questions.md) (karara bağlanmamışlar).
Nasıl çalıştığı: [docs/plan/README.md](docs/plan/README.md).

⚠️ [docs/handoff.md](docs/handoff.md) §10 yol haritası **Admin Dashboard'u 1.
sıraya** koyar; bu bilinçli olarak değiştirildi (dashboard M6). Gerekçe:
[roadmap.md](docs/plan/roadmap.md#neden-dashboard-1-değil-6-sırada). Handoff'un
ürün içeriği geçerli, değişen yalnızca sıra.
