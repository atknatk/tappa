# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-07-24

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | M0 — [Bootstrap](m0-bootstrap.md) |
| **Sıradaki görev** | **M0-03** (Docker bekliyor) → değilse **M0-07** (Q26 bekliyor). M0 kalanı kullanıcı girdisine bağlı. |
| **Dal** | `m0-bootstrap` — ilk commit `7e12f37` `main`'de; M0'ın kalanı dalda, M0 bitince `main`'e birleşir (CLAUDE.md §10) |
| **Blokeler** | **Docker daemon kapalı** → M0-03 için kullanıcı Docker Desktop'ı başlatmalı |

**Bir sonraki oturum ne yapmalı:** M0'ın kalan üç görevi de kullanıcı girdisi
bekliyor: M0-03 → Docker Desktop · M0-07 → Q26 (Go ≥1.26.5, arm64) · M0-06 →
Q04 + M0-07. Girdi gelmeden M1'e geçilmez (M1-02 ayrıca Q01'e bağlı).

**Not:** M0-05 (ilk commit) sıradan **öne alındı** — kullanıcı "arada commit at"
dedi. Bundan sonra her onaylanan görevin ardından bir commit atılır.

**Politika ve kapsam kararları: hepsi karara bağlandı** — Q14…Q24 cevaplandı,
gerekçe ve etkilenen kartlar [open-questions.md](open-questions.md) →
Cevaplananlar'da. M3 ve M6 bekleyen karar olmadan yazılabilir.

**Kalan açık sorular (Q01–Q13, Q25, Q26)** teknik/ticari; hiçbiri M0'ı bloklamıyor.
En yakın blokajlar: Q01 (zaman dilimi) → M1-02, Q04 (DB testi) → M0-06,
Q25 (küçük araç düzeltmeleri) → M0-04, Q26 (Go toolchain) → M0-07.

**Kabul edilen riskler** (ADR 0005, [M3-09](m3-policy-motoru.md)): buddy
punching · sahte GPS · **URL biriktirme** · mekânda proxy · müdürün kimlik
basması. Hiçbiri çözülmedi; hepsinin tespit sinyali [M6-11](m6-dashboard.md)'de.

---

## Sağlık kontrolü

İşe başlamadan çalıştır. Beklenen çıktıyı vermeyen bir satır varsa **önce onu
düzelt**.

| Komut | Beklenen |
|---|---|
| `go version` | `go1.26.2` veya üstü |
| `go build ./...` | çıktı yok (temiz) |
| `git log --oneline \| head -3` | M0-05 sonrası en az 1 commit |
| `git status --short` | M0-05 sonrası temiz |
| `ls .env` | M0-01 sonrası var (git'e **girmez**) |
| `docker compose ps` | M0-03 sonrası `tappa-db` ayakta |
| `make migrate-status` | M1 boyunca uygulanan migration listesi |
| `make check` | M0-06 sonrası yeşil |

---

## Ledger

Durumlar: `todo` · `wip` · `done` · `blocked` · `skipped`
Bir görev `done` olurken commit hash'i yazılır. `blocked`/`skipped` ise **neden**
yazılır.

### M0 — [Bootstrap](m0-bootstrap.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M0-01 | .env ve kriptografik anahtarlar | **done** | commit yok (`.env` ignore'da) · üçüncü göz 2. turda ONAY · kart düzeltildi (F2) |
| M0-02 | Go bağımlılıkları (pgx, uuid, templ) | **done** | `e6d9a63` · üçüncü göz 3. turda ONAY · kart iki kez düzeltildi |
| M0-03 | Postgres ve rol ayrımı doğrulaması | blocked | Docker daemon kapalı |
| M0-04 | Üretim hattı doğrulaması (templ · sqlc · tailwind) | **done** | üçüncü göz 2. turda ONAY · `sqlc.yaml`'da 3 bozuk override bulundu ve düzeltildi |
| M0-05 | İlk commit ve dal stratejisi | **done** | `7e12f37` · sıradan öne alındı (kullanıcı isteği) · orkestratör yaptı, M0-02 denetiminde doğrulanacak |
| M0-06 | CI iş akışı | todo | Q04 · M0-07'ye bağlı |
| M0-07 | make check ve make audit'i yeşile alma | todo | Q26 · denetim bulgusu (SA1019 + stdlib CVE) |

### M1 — [Veri katmanı](m1-veri-katmani.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M1-01 | ADR 0002: tenant bağlamı ve RLS stratejisi | todo | |
| M1-02 | Migration 0001: tenants | todo | Q01 |
| M1-03 | Migration 0002: locations & departments | todo | Q01, Q07 |
| M1-04 | Migration 0003: employees & sessions | todo | |
| M1-05 | Migration 0004: tags | todo | |
| M1-06 | Migration 0005: transactions (append-only) & audit_log | todo | |
| M1-07 | internal/db: havuz ve tenant kapsamlı transaction | todo | |
| M1-08 | İlk sqlc sorguları | todo | |
| M1-09 | RLS izolasyonu ve değişmezlik testleri | todo | Q04 |
| M1-10 | Seed verisi ve sabit ID'ler | todo | |
| M1-11 | Migration 0006: admin kullanıcıları | todo | Q03 · denetim bulgusu |

### M2 — [SUN doğrulama](m2-sun.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M2-01 | ADR 0003: SDM modu ve anahtar yönetimi | todo | Q05, Q06 |
| M2-02 | AES-CMAC (RFC 4493) | todo | |
| M2-03 | SDM URL ayrıştırma | todo | |
| M2-04 | Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma | todo | |
| M2-05 | Anahtar sarmalama (KEK) | todo | |
| M2-06 | Atomik sayaç ilerletme ve eşzamanlılık testi | todo | **§4.4 — en kritik** |
| M2-07 | sun.Verify ve test vektörleri | todo | kapsam %90+ |

### M3 — [Policy motoru](m3-policy-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M3-01 | ADR 0004: policy motoru modeli | todo | |
| M3-02 | Policy şeması (append-only sürümler) | todo | |
| M3-03 | Belge modeli, ayrıştırma ve doğrulama | todo | |
| M3-04 | Değerlendirici (koşullar, öncelik, açıklanabilirlik) | todo | |
| M3-05 | Guardrail politikaları | todo | **§4 — en kritik** |
| M3-06 | Tappa Baseline yönetilen politikası | todo | A2, A3, Y1 varsayılanları |
| M3-07 | Kararın kayda bağlanması | todo | |
| M3-08 | Test seti ve gevşetilemezlik kanıtı | todo | kapsam %90+ |
| M3-09 | ADR 0005: kabul edilen riskler | todo | Q19 — buddy punching, sahte GPS |

### M4 — [Tap karar motoru](m4-tap-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M4-01 | internal/geo: haversine ve yarıçap | todo | |
| M4-02 | Karar girdi/çıktı tipleri | todo | |
| M4-03 | Decide(): bağlam kurma ve kararın uygulanması | todo | M3-04 üstünde |
| M4-04 | Yön tayini (in/out) | todo | |
| M4-05 | Vardiya çözümü ve geç kalma | todo | Q01 |
| M4-06 | Trust puanı, QR kanalı, practice tap | todo | |
| M4-07 | Tablo bazlı test seti | todo | kapsam %90+ |

### M5 — [Tap akışı](m5-tap-akisi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M5-01 | internal/session: oturum yaşam döngüsü | todo | Q11 |
| M5-02 | Davet ve aktivasyon akışı | todo | Q02 |
| M5-03 | Middleware: gerçek IP, tenant, oran sınırı | todo | |
| M5-04 | GET /t: tap sayfası | todo | skill tappa-brand |
| M5-05 | POST /api/checkin: orkestrasyon | todo | **§4.3/4.4/4.6** |
| M5-06 | Onay ekranı ve marka mesajları | todo | |
| M5-07 | Mini tur ve practice tap | todo | |
| M5-08 | QR kanalı | todo | |
| M5-09 | Uçtan uca test ve "bir günü simüle et" | todo | |
| M5-10 | Tap tazelik penceresi (URL biriktirmeye karşı) | todo | A1 · §4.4 |

### M6 — [Admin dashboard](m6-dashboard.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M6-01 | Admin kimlik doğrulama | todo | Q03 |
| M6-02 | Dashboard iskeleti ve docket bileşenleri | todo | |
| M6-03 | Transactions sekmesi | todo | |
| M6-04 | FLAGGED onay kuyruğu | todo | **§4.3** |
| M6-05 | Employees sekmesi | todo | |
| M6-06 | Locations & Wall Tags sekmesi | todo | |
| M6-07 | Reports ve CSV export | todo | |
| M6-08 | Manuel kayıt girişi | todo | |
| M6-09 | Policy yönetim ekranı | todo | guardrail kilitli gösterilir |
| M6-10 | Policy simülatörü | skipped | Q22 → M9-06'ya ertelendi |
| M6-11 | Anomali ve kötüye kullanım raporu | todo | A1·A3·A4·Y-D·Y-E sinyalleri |
| M6-12 | Çalışan sayımı ve fatura taslağı | todo | Q24 |

### M7 — [Portal & signup](m7-portal.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M7-01 | Landing sayfası | todo | |
| M7-02 | Kayıt sihirbazı ve VAT | todo | Q09 |
| M7-03 | Tenant provisioning | todo | |
| M7-04 | Admin daveti, şifre sıfırlama, e-posta | todo | Q02 |
| M7-05 | Hesap ve marka mesajı ayarları | todo | |

### M8 — [Deploy & pilot](m8-deploy-pilot.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M8-01 | Paketleme | todo | |
| M8-02 | Barındırma | todo | Q08, Q12 |
| M8-03 | Gözlemlenebilirlik | todo | |
| M8-04 | Güvenlik denetimi | todo | |
| M8-05 | Plaket encode runbook | todo | Q06, Q10 |
| M8-06 | KF St Julians pilotu | todo | Q13 · yasal pilot kapısı |
| M8-07 | Üretim tenant kurulumu ve cihaz envanteri | todo | denetim bulgusu |

### M9 — [Pilot sonrası](m9-sonrasi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M9-01 | Çevrimdışı kuyruk | todo | MVP dışı |
| M9-02 | Yönetici push bildirimleri | todo | MVP dışı |
| M9-03 | BioTime CSV içe aktarma | todo | MVP dışı |
| M9-04 | Tenant marka mesajı editörü | todo | MVP dışı |
| M9-05 | Çalışan self-service saat görünümü | todo | MVP dışı |
| M9-06 | Policy simülatörü | todo | Q22 — M6-10'dan ertelendi |
| M9-07 | Ham JSON politika editörü | todo | Q22 — M6-09'dan ayrıldı |

**Özet:** 82 görev · done 4 · wip 0 · blocked 1 · skipped 1 · todo 76

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

### 2026-07-24 — dış denetim (3 ajan) ve bulguların işlenmesi

Kod yazılmadı. Plan üç bağımsız ajana okutuldu: **tutarlılık**, **güvenlik**,
**pratiklik**. Bulguların tamamı [open-questions.md](open-questions.md) →
"İkinci denetim" tablosunda, nereye işlendikleriyle birlikte.

**En önemli sonuç: A1 (URL biriktirme) çözülmemişti.** M5-10 tazelik penceresi
`GET /t` anından başlıyor; saldırgan uçak modunda 10 kez dokunup URL'leri
toplayabiliyor — sunucu o okumaları hiç görmüyor. Önceki oturumda "✅ çözüldü"
işaretlemem yanlıştı, düzeltildi. A1 artık **kabul edilen risk** (ADR 0005) +
`tap:ctrGap` sinyali (Q21).

Diğer üç yapısal bulgu: `occurred_at` istemciden geliyor ve guardrail'siz (K1) ·
motor yetkilendirmede fail-open (K2) · tenant çözümlemesi RLS bağlamından önce
gelmek zorunda (K3). Üçü de karşılandı: **altı yeni guardrail** eklendi
(`sys:tenant-mismatch`, `sys:occurred-at-bound`, `sys:policy-edit-owner-only`,
`sys:no-self-review` + `ignore`/`redirect` kilidi + guardrail **sırası** normatif).

Dört karar (Q21–Q24): A1 politikaya · M3 v1 daraltıldı (simülatör ve JSON
editörü M9'a) · yasal metinler dağıtıldı + **pilot kapısı** · tahsilat elle,
sayım otomatik.

Yeni görevler: M1-11 (admin şeması — hiç yoktu), M6-12 (fatura taslağı),
M8-07 (üretim tenant + telefon envanteri), M9-06/07 (ertelenenler).
M6-10 `skipped`. Görev sayısı 76 → **81**.

Sırada: M0-01. Açık soru sayısı 13 → **14** (Q25 küçük araç düzeltmeleri).

### 2026-07-24 — policy motoru plana eklendi, milestone'lar kaydırıldı

Kod yazılmadı. Plan gözden geçirildi; kötüye kullanım analizinde dört ciddi açık
(URL biriktirme, QR + sahte GPS, GPS sahteciliği, buddy punching) ve yedi
mantık boşluğu bulundu — özeti [open-questions.md](open-questions.md) A/Y
maddelerinde.

Çözüm olarak **policy motoru** (AWS IAM benzeri belge yapısı, üç katman:
guardrail / baseline / tenant) yeni **M3** milestone'u olarak eklendi; eski
M3–M8 birer basamak kaydı (M4–M9). Hiç commit ve tamamlanmış görev olmadığı için
yeniden numaralandırma bedelsizdi. Görev sayısı 63 → **75**:
M3 (8 yeni) · M5-10 tazelik penceresi · M6-09/10/11 policy ekranı, simülatör,
anomali raporu.

Tap kararları artık kod içi `if` zinciri değil: §5 satır 1–5 **guardrail**
(kapatılamaz), satır 6–7 **baseline** (tenant değiştirebilir). `tap.Decide`
bağlam kurar ve effect'i uygular.

Aynı oturumda **yedi karar** alındı ve işlendi (Q14–Q20):
WiFi adımı · QR'da IP zorunlu · GPS-only tenant anahtarı · çapraz lokasyonda
tap edilen lokasyonun vardiyası · **unutulan çıkışta otomatik kapatma YOK**
(açık kayıtlar saat toplamına girmez, rapor eksikliği açıkça söyler) ·
buddy punching kabul edilen risk (ADR 0005 → yeni **M3-09**) · onaylar ayrı
`transaction_reviews` tablosunda.

Etkilenen kartlar güncellendi: M1-06, M3-06, M3-09 (yeni), M4-04, M4-05, M5-02,
M5-08, M6-04, M6-07, M8-06. CLAUDE.md §5'e geç kalma, unutulan çıkış ve QR
maddeleri işlendi. Görev sayısı 75 → **76**.

Sırada: M0-01.

### 2026-07-24 — planlama altyapısı kuruldu

Kod yazılmadı. `docs/plan/` oluşturuldu: roadmap, 63 görev kartı (o günkü
numaralandırmayla M0–M8; policy motoru eklenince kaydı),
bu durum dosyası, 13 açık soru. Yol haritası sırası handoff §10'dan bilinçli
olarak farklı — gerekçe [roadmap.md](roadmap.md#neden-dashboard-1-değil-6-sırada).

Repo durumu: iskelet dosyalar var ve derleniyor (`go build ./...` temiz), ama
`db/migrations`, `db/queries`, `web/templates` boş; `internal/` altında yalnız
`config` ve `httpx` var. Commit geçmişi yok, `.env` yok, Docker kapalı.

Sırada: M0-01.
