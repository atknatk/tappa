# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-07-26 (4. oturum — compact sonrası devam; **M3 TAMAM + M4-01,M4-02 done**)

> **▶️ M4 DEVAM — 2026-07-26, 4. oturum.** M3 (policy motoru) 9/9 TAMAM. **M4-01** (internal/geo)
> `f791f91` · **M4-02** (karar tipleri) `860fcd8` — ikisi de üçüncü göz ONAY. Her şey `main`'de,
> ağaç temiz, `make check`+`make audit` yeşil. **Sıradaki:** "ŞU AN" → **M4-03** (Decide: bağlam
> kurma + kararın uygulanması — M3-04 değerlendiricisi üstünde, §4.6/§5/R8 kritik, iki denetçi).
> M4/M5'e devredilen notlar (N1–N4 + ErrUnknownTag) aşağıda "M4/M5'e devralınan"da. **⚠️ M4-03 açık
> tasarım noktası:** `Decide(in Input)` policy.Set'i nasıl alıyor (Input'a alan mı, parametre mi —
> M4-02 imzası Set içermiyor); M4-03 çözer + kartı düzeltir. Bekleyen kullanıcı kararı YOK. Kritik durum sohbette kalmıyor.

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | **M0 + M1 + M2 + M3 TAMAM** ✅ · **M4 devam** — M4-01, M4-02 done (2/7). → **[M4 tap karar motoru](m4-tap-motoru.md)** |
| **Sıradaki görev** | **M4-03** — [Decide(): bağlam kurma ve kararın uygulanması](m4-tap-motoru.md#m4-03--decide-bağlam-kurma-ve-kararın-uygulanması) (§4.6 · §5). Kanıtları `policy.Context`'e çevir → `policy.Evaluate` çağır → effect→verdict (allow→ok, review→flag, deny→reject, ignore→ignored); **`sys:no-session` özel: kayıt YAZILMAZ, Redirect** (§5.3 tek istisna). IPMatch/GPSMatch/gpsDistanceM/ctrGap hesapla (geo.WithinRadius, SourceIP∈LocationIPs). **§5 satır 1–5 guardrail, 6–7 baseline olarak policy'den çözülür — `if` zinciri YAZMA** (motor delege). **⚠️ Set nasıl geliyor:** M4-02 `Decide(in Input)` Set içermiyor → Input'a `policy.Set` alanı ekle VEYA parametre; çöz + M4-02 kartını düzelt. **R8/§4.6 tuzakları:** deaktive kontrolü SUN'dan önce=bilgi sızıntısı, debounce SUN'dan önce=replay (ama sıra artık M3-05 guardrail'de — Decide erken kısa-devre YAPMAMALI). Satır 7 asla reject değil (§4.6). Kırmızı çizgi §4.6/§5 → agent tappa-security-auditor R8 + iki denetçi. Bekleyen kullanıcı kararı YOK. |
| **Çalışma modu** | Orkestrasyon + üçüncü göz — [README.md](README.md) · brief'ler [agent-brief.md](agent-brief.md) |
| **Dal** | **`main`** — M0 (`m0-bootstrap`) `main`'e fast-forward birleştirildi (`562f021`), dal silindi. **Kullanıcı kararı (2026-07-25): artık doğrudan `main`'de çalışılır, görev başına dal açılmaz** (CLAUDE.md §10 güncellendi). Push/PR yine istemedikçe yok. |
| **Blokeler** | Yok (M2-02 için bekleyen karar yok). **Bekleyen kullanıcı eylemi:** arm64 Go kurulumu (aşağı bak) — hiçbir şeyi bloklamıyor. |

**Bir sonraki oturum ne yapmalı:** **M2-02** … **[TAMAMLANDI — M2 kapandı, aşağıki "ŞU AN" M3-02'yi gösterir]**.
M3 sırası: M3-02 (şema) → M3-03 (belge modeli + doğrulama) → M3-04 (değerlendirici) → **M3-05
(guardrail'ler, §4 en kritik)** → M3-06 (baseline) → M3-07 (kararın kayda bağlanması) → M3-08
(gevşetilemezlik kanıtı, kapsam %90+) → M3-09 (ADR 0005 kabul edilen riskler).

### M3-04'e devralınan (M3-01 denetiminden, bloklamayan)
1. **M3-04 kartındaki `Decision` struct yorumu** ([m3-policy-motoru.md](m3-policy-motoru.md) ~satır 251)
   effect'leri `allow | review | deny | ignore` sayıp **`redirect`'i atlıyor** — oysa değerlendirici
   `redirect` de döndürür (`sys:no-session`, `sys:tenant-mismatch`). Kartın önceden var olan küçük
   hatası, M3-01 kapsamı dışıydı; **M3-04 yapılırken kart düzeltilmeli** (agent-brief madde 6).
2. ADR 0004, **değerlendirme anındaki** bilinmeyen operatör/anahtar (sürüm geri-alma sonrası) davranışını
   açıkça yazmıyor; M3-04 kartı yazıyor (ifade **eşleşmez**, koşul atlanmaz — yoksa deny koşulsuzlaşır).
   ADR bununla çelişmiyor ("sessizce yok sayma yok / kısıtlayıcıya düş" doğru yönde). M3-04'te uygula.

### M3-05'e devralınan (M3-03 denetiminden, bloklamayan)
- **Bounded-param üretimde BOŞ.** `internal/policy/validate.go` bounded-param mekanizmasını kurdu ve test
  etti (enjekte edilen aralık → aralık-dışı reddedilir), ama `DefaultLimits().BoundedParams = nil` →
  üretim yolunda ADR §11 koruması **fiilen yok** (değerlendirme henüz olmadığından M3-03/M3-04'te açık
  yaratmaz). **M3-05 doldurmalı.** Denetçi düzeltmesi: eşlenebilir anahtar **ÜÇ** (`tap:gpsDistanceM`,
  `tap:pageAgeSeconds`, **`tap:occurredAtSkewSeconds`** — ADR §11 occurred_at sapması 0–72 sa) + debounce
  (bağlam anahtarı YOK, yalnız config/guardrail param). M3-05 üçünü de + config sınırlarını (GPS 25–1000 m,
  tazelik 1–15 dk, sapma 0–72 sa, debounce 30–300 sn) doldurmazsa koruma **sessizce eksik** kalır.
- **(M3-04 denetiminden)** `internal/policy/evaluate.go:169` bir yorumda kartı alıntılayan **Türkçe** ifade var
  (§7 gri alan: yorum, identifier/log/hata/commit sayılmaz → bloklamadı). internal/policy'ye bir sonraki
  dokunuşta (M3-05/M3-08) İngilizce'ye çevir.

### M3-07'ye devralınan (M3-04 denetiminden, bloklamayan)
- **Default kararı `Layer=guardrail` taşıyor**, `MatchedSid="default"` ile ayrılıyor (kodda gerekçeli — dördüncü
  Layer değeri uydurulmadı). M3-07 raporlama/kayıt yolunda guardrail'i default'tan **`matched_sid`** ile ayırmalı
  (guardrail kararında `policy_version_id` boş + `matched_sid="sys:…"`; default'ta `matched_sid="default"`).

### M4/M5'e devralınan (M3-05 denetiminden — guardrail'lerin girdi sözleşmesi)
Guardrail'ler saf `policy.Evaluate` girdisine güvenir; bu girdiyi M4 (`tap.Decide` bağlam kurar) / M5 (handler)
DOLDURUR. Aşağıdakiler doldurulmazsa guardrail **sessizce** ateşlemez (eksik anahtar ≠ false, M3-04 invariant'ı):
- **N1 — `tap:sunValid`:** M5 her NFC tap'inde bunu set etmeli, yoksa `sys:sun-invalid` sessiz kalır (asıl atomik
  ctr koruması `internal/sun` M2-06'da; guardrail onun policy-katmanı yansıması — ikisi birlikte).
- **N2 — `tap:channel` SUNUCU-türetimi:** `channel` `ctr`/`cmac` varlığından türetilmeli (istemci beyanından
  DEĞİL — ADR 0004 §8). sun-invalid/freshness'in "NFC-only" kapsaması buna dayanır; istemci `channel=qr`
  diyip SUN korumasını atlayamamalı.
- **N3 — debounce değer akışı:** `TAPPA_DEBOUNCE_SECONDS` aralık-kontrollü (M3-05) ama henüz `policy.Params`'a
  **bağlanmadı** (`DefaultParams` debounce=60 sn sabit). M4/M5 config değerini Params'a bağlamalı; bağlanana
  kadar küçük drift riski (bloklamıyor — sınırlar ortak).
- Ayrıca (M2'den): `sun.Verify` `ErrUnknownTag` döndürür → M4/M5 bunu yutmamalı, global güvenlik olayı loglamalı.
- **N4 (M3-07 denetiminden) — M5-05 yazma yolu Decision→sütun sadakati:** `transactions_policy_decision_consistent`
  CHECK + composite FK §4.6'yı ancak M5-05 `policy.Decision`'ı sütunlara sadık eşlerse korur. Bir çağıran
  baseline/tenant için `Policy.VersionID`'yi `uuid.Nil` ile yüklerse: pointer non-nil olur → CHECK branch (c)
  geçer ama FK `23503` verir → **kayıt kaybı**. Evaluate/baseline.go bugün bunu ASLA üretmez (gerçek version
  id yükler); bu tam da CHECK+FK'nin erken yakalamak için var olduğu wiring-bug sınıfı. **M5-05 yazma yolunda
  ve denetiminde:** baseline/tenant kararında gerçek `policy_version_id` yüklendiğini + policy_context'in ham
  GPS değil mesafe taşıdığını (§4.7) doğrula.

### M2-04'e devralınan not (M2-01 denetiminden, N3)
SV2 içindeki `ctr`'nin byte sırası ADR/skill'de açıkça sabitlenmedi (bilinçli) → **M2-04/M2-07
bilinen-cevap vektörleriyle** sabitlenmeli (little vs big-endian sessizce yanlış "makul" değer üretir).

### M6'ya devredilen (M1-11 denetiminden) — back-FK boşluğu
`transactions.entered_by` · `transaction_reviews.reviewer_id` · `audit_log.actor_id` →
`admin_users` FK'leri **eklenmedi** (00005 yorumları "M1-11'de eklenir" demişti — **artık
yanıltıcı**, 00005 immutable/düzeltilemez). M1-11 kartı bunları istemiyor + `reviewer_id NOT NULL`
FK'si rls_test fixture'ını (rastgele reviewer_id) kırardı. **M6-04 (review akışı) / M6-01 (auth)**
bu back-FK'leri (composite same-tenant) + fixture yeniden yazımını yapar. Sınırlı risk: yazım
yolları henüz yok, reviewer_id self-review trigger'ıyla korunuyor. `actor_id` polimorfik
(admin|employee) → tek FK doğru değil, ayrı ele alınır.

### M6 handler denetimine not (M1-11'den)
`store.AdminUser.PasswordHash`/`TokenHash` üretilen struct'larda — handler'da `%+v`/slog ile
loglanırsa §7 sır sızıntısı. M6-01 handler denetiminde kontrol et.

### Devam eden düşük notlar
- **Dev-DB test kalıntısı birikiyor** (M3-02 security-auditor bulgusu): `internal/db/rls_test.go`
  random-UUID fixture'ları COMMIT ediyor ve `policy_versions`/`transactions` append-only + REVOKE DELETE
  olduğundan app-katmanı teardown **tasarımca imkânsız** (M1-09: imkânsızlık = garanti). Sonuç: her
  `make test` koşusu tenants/policies/... satırı ekliyor (auditor: tenants≈1089). Kırmızı çizgi DEĞİL,
  yalnız hijyen; demo/prod öncesi `make db-reset`. İstenirse owner-teardown veya testcontainers ile
  izole DB (M8 deploy denetimi) çözer.
- `password_resets.used_at`/`admin_sessions.revoked_at`/`expires_at` UPDATE-edilebilir (append-only
  trigger yok); tek-kullanımlık/iptal bütünlüğü **app katmanında** (M6/M7 sorguları). sessions.revoked_at
  ile aynı desen; istenirse M6'da immutability trigger'ı defense-in-depth eklenebilir.
- **aes_key_ref KEK-sarmalı doğrulaması** (M1-05'ten): şema bytea zorlayamaz → insert-yolu (M2/M5)
  + seed KEK-sarmalı bekler; KEK DB dışında (config `TAPPA_TAG_KEK`) — M8 deploy denetimi.

**M1-03'ten devralınan iki not (bloklamayan, yapıcının eklediği ekstra kısıtlar):**
- **M4-05:** `locations.shift_*` nullable → geç kalma hesabı null vardiyayı "hesaplanmaz"
  ile ele almalı. Ayrıca `shift_pair` CHECK **tek-yönlü vardiyayı** (yalnız shift_start)
  reddediyor — ileride esnek-saat lokasyonu gerekirse bu kısıt gözden geçirilir.
- **M1-10:** seed tüm lokasyonlarda **çift-uçlu** vardiya kullanmalı (shift_pair CHECK).
  Ufak tutarsızlık: `overnight=true` + NULL vardiya migration'da kabul ediliyor (zararsız,
  domain yok sayar); seed overnight'ı yalnız dolu vardiyayla kullansın.

**Migration numaralandırma:** goose `-s` (sequential), 5 haneli — `00001_...`,
`00002_...`. Makefile `migrate-new` artık `-s` geçiyor (M1-02'de düzeltildi).

**⚠️ Planlı kırmızı durum (M1-02→M1-07):** `make gen`/`make dev`/`make build` sqlc
adımında **"no queries contained in paths"** ile patlar — sorgular M1-08'e ait
(M1-08 ledger notu: "ilk sorgu bunları yeşile çevirir"). **`make check` bundan
etkilenmez** (fmt+lint+test+temiz-diff; sqlc çalıştırmaz) ve CI yeşil kalır. Migration
doğrulaması sqlc'ye değil goose+psql'e dayanır. Bu regresyon değil, plan sonucu.

**M1'e girmeden önce hazır olması gerekenler** (hepsi çözüldü):
Q01 (timezone) ✔ · Q04 (yerel Postgres) ✔ · Q27 (`NULLIF`) ✔.

### M1-05 için devralınan gereksinim (ADR 0002 madde 7 — M1-04'te kurulan kalıp)

Çözümleme yolu **çevrelenmiş (bounded) bypass** olarak M1-04'te sessions için
kuruldu ve iki denetçi (üçüncü göz + tappa-security-auditor) tarafından ampirik
onaylandı. **M1-05 tags ayağını AYNI kalıpla kurar:**
- `tappa_resolver` rolü **zaten var** (db-init: NOLOGIN, BYPASSRLS, NOSUPERUSER,
  default privilege YOK). M1-05 ona **`tags`'te sütun-düzeyi SELECT** verir (yalnız
  çözümleme için gerekenler) + `resolve_tag_by_uid(...)` SECURITY DEFINER fonksiyonu:
  **owner tappa_resolver** (superuser DEĞİL), **`SET search_path=pg_catalog, pg_temp`**,
  gövde `public.tags` nitelenmiş, **`REVOKE ALL ... FROM PUBLIC` + yalnız tappa_app'e
  EXECUTE**, `uid` PK → ≤1 satır. Fonksiyon ihtiyaçtan fazla sütun döndürmesin.
- tags RLS politikası **standart NULLIF** — resolve OR-dalı **YOK** (GUC-anahtar saf-RLS
  alternatifi denetimde reddedildi: SET LOCAL'siz GUC havuzda kalıp toplamsal OR ile
  çapraz-tenant sızdırıyor + `FOR ALL USING` WITH CHECK'i kopyalıyor; ADR 0002 madde 7
  ve "Değerlendirilen alternatifler"e kaydedildi).
- Güvenlik RLS'ten değil **arayüzden**: beş kısıt (anahtar girdi · ≤1 satır · SELECT*
  yüzeyi yok · yalnız EXECUTE · naif "NULL iken satır" dalı yasak). Sınırı M1-09'da test.

### DELETE tuzağı — M1-05, M1-06 ve immutability isteyen her tablo (M1-04'te bulundu)

`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner ... GRANT ... DELETE ... TO tappa_app`
(db-init) **her yeni tabloda** tappa_app'e DELETE'i otomatik verir. Bir tablodan silmeyi
engellemek için GRANT'tan DELETE'i çıkarmak **YETMEZ** (GRANT yalnız ekler) — açık
**`REVOKE DELETE ON <tablo> FROM tappa_app;`** gerekir (M1-04 sessions/employees böyle
yaptı). **M1-06 `transactions`/`audit_log` için bu ZORUNLU** (§4.3 immutable: `REVOKE
UPDATE, DELETE` + trigger). Ampirik doğrulandı: REVOKE'suz DELETE başarıyla koşuyordu.

### ⏳ Bekleyen kullanıcı eylemi — arm64 Go kurulumu

Q26 kararı: yerel toolchain arm64'e geçecek. **Repo işini bloklamıyor** — her şey
amd64 Go 1.26.5 ile yeşil; kazanç yalnız yerel derleme/test hızı (Rosetta ~2-3x
yavaş). Orkestratör go.dev tarball'ını indirdi ve **checksum'ı go.dev ile birebir
doğruladı** (`efb87ff2…`), ama `/usr/local`'a kurulum **sudo parolası** ister —
kullanıcı çalıştırmalı. Komutlar oturum notunda. Yapılınca `go version` →
`darwin/arm64` olur ve ilk `make gen` bir kez uzun sürer (build cache + pinli CLI
önbellekleri tazelenir — bozukluk değil).

**Not:** M0-05 (ilk commit) sıradan **öne alındı** — kullanıcı "arada commit at"
dedi. Bundan sonra her onaylanan görevin ardından bir commit atılır.

**Politika ve kapsam kararları: hepsi karara bağlandı** — Q14…Q27 cevaplandı,
gerekçe ve etkilenen kartlar [open-questions.md](open-questions.md) →
Cevaplananlar'da.

**Kalan açık sorular (Q02, Q03, Q05–Q13)** teknik/ticari; hiçbiri M0'ı veya M1'in
başını bloklamıyor. En yakın blokajlar: Q07 (`static_ips` tipi) → M1-03,
Q03 (admin şifre hash'i) → M1-11/M6-01, Q05+Q06 (SDM modu, anahtar stratejisi) → M2-01.

### M1-09 için devralınan bulgular (M0-03 3. tur denetiminden)

Denetçi kabul kriterini yenen **üç kaçış yolu** buldu. Bloklayan sayılmadılar
(kriter bugünkü hâliyle de M0-03'ün gerektirdiğinden fazlasını yapıyor), ama
M1-09 brief'ine **girmeleri zorunlu** — yoksa yeşil ve anlamsız bir test seti çıkar:

1. **grep sağlam değil.** `tenant_id =` taraması `'<B>'::uuid = tenant_id`,
   `tenant_id IN ('<B>')`, `tenant_id::text = '<B>'` biçimlerini kaçırıyor; üçü de
   RLS **kapalıyken de** 0 satır veriyor. Bağlayıcı olan düzyazı şart, grep işaret.
2. **Pozitif kontrol istenmiyor.** Vaka 3 boş tabloda kritere tam uyar ve hiçbir şey
   kanıtlamaz. Her izolasyon vakası, aynı ham sorgunun **doğru bağlamda >0** döndüğünü
   de göstermeli; korumayı kapatınca test **kırmızıya dönmeli**.
3. **Rol boyutu çalışma anında kanıtlanmıyor.** Kriter `appPool`/`ownerPool`
   **adlandırmasına** bakıyor; owner kimlikli havuzu `appPool` diye adlandıran test
   geçer. Doğrusu: testin içinde `SELECT current_user` = `tappa_app` **ve**
   `rolsuper/rolbypassrls = f,f` assertion'ı (ikisi de `tappa_app` ile çalışıyor).

Ayrıca kapsam dışı iki tutarsızlık: `scripts/db-init/01-roles.sql:3` ve
[m0-bootstrap.md:59](m0-bootstrap.md) bypass'ı "tablo sahibi + BYPASSRLS" diye
anlatıyor, **superuser'dan söz etmiyor** ve `FORCE`'un salt sahipliği yendiğini
yazmıyor (ölçüldü). Güvenlik sonucu yok, temkinli yönde yanlış — M1-01 ADR 0002
yazılırken düzeltilmeli.

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
| M0-03 | Postgres ve rol ayrımı doğrulaması | **done** | üçüncü göz **3. turda** ONAY · kart düzeltildi · iki ölçüm M1'i bağladı (→ Q27, ve M1-01/M1-02/M1-09 kartları güncellendi) |
| M0-04 | Üretim hattı doğrulaması (templ · sqlc · tailwind) | **done** | `2521d48` · üçüncü göz 2. turda ONAY · `sqlc.yaml`'da 3 bozuk override bulundu ve düzeltildi |
| M0-05 | İlk commit ve dal stratejisi | **done** | `7e12f37` · sıradan öne alındı (kullanıcı isteği) · orkestratör yaptı, M0-02 denetiminde doğrulanacak |
| M0-06 | CI iş akışı | **done** | üçüncü göz **1. turda** ONAY · `make up`+`make check`+`make audit`, Go 1.26.5 pinli, ripgrep kurulu, Node yok · iki kart sapması ölçümle doğrulandı (`CGO_ENABLED=1`, `services:` yerine `make up`) |
| M0-07 | make check ve make audit'i yeşile alma | **done** | üçüncü göz **2. turda** ONAY · SA1019 (RealIP çıkarıldı) + Q25 a/b/d · redline R5 üç sessiz atlatma turunda yeniden yazıldı (lexer) · **arm64 hâlâ açık** (aşağı bak) |

### M1 — [Veri katmanı](m1-veri-katmani.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M1-01 | ADR 0002: tenant bağlamı ve RLS stratejisi | **done** | `4eb3780` · üçüncü göz **2. turda** ONAY (1. tur RED: madde 7 superuser SECURITY DEFINER çelişkisi) · Q27 (`NULLIF`) + M0-03 superuser/FORCE ölçümleri normatif · iki tutarsızlık (01-roles.sql, m0-bootstrap.md) + kart madde 7 örneği düzeltildi |
| M1-02 | Migration 0001: tenants | **done** | `aff4ced` · üçüncü göz **1. turda** ONAY · RLS beşlisi (id-PK istisnası) canlı doğrulandı, policy birebir `NULLIF`, fail-closed/WITH CHECK/pozitif kontrol tappa_app ile geçti, Down çalışıyor, R5 mutasyonla kanıtlandı · Makefile `migrate-new` `-s` düzeltmesi · kart adım 3 NULLIF'e güncellendi |
| M1-03 | Migration 0002: locations & departments | **done** | `3d66b17` · üçüncü göz **1. turda** ONAY · RLS beşlisi (iki tablo) + çapraz-tenant bileşik FK + `cidr[]` (Q07) + `numeric(9,6)` + Down + R5 mutasyonla kanıtlandı · 2 bloklamayan kısıt notu (→ M4-05/M1-10) · Q25(c) sqlc override M1-08'e ertelendi |
| M1-04 | Migration 0003: employees & sessions | **done** | `2c42c67` (+ db-init resolver rölü) · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · ADR 0002 madde 7 çözümleme mekanizması: `tappa_resolver` (BYPASSRLS) + `resolve_session_by_token_hash` SECURITY DEFINER (owner non-superuser, search_path sabit, PUBLIC REVOKE, kolon-SELECT) — enumerate/search_path/PUBLIC/injection saldırılarına dayandı · **GUC-anahtar alternatifi denetimde reddedildi** (ADR'ye kaydedildi) · sessions/employees DELETE `REVOKE` edildi (default-privilege tuzağı) · ADR 0002 + M1-04 kartı güncellendi |
| M1-05 | Migration 0004: tags | **done** | `a1bcdc4` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · `resolve_tag_by_uid` çözümleme fonksiyonu (M1-04 kalıbı; enumerate/pg_temp-poison/PUBLIC/injection saldırılarına dayandı) · uid `char(14)` hex CHECK · `aes_key_ref bytea` sarmalı · **`UNIQUE(uid,ctr)` YOK** (§4.4) · DELETE REVOKE · replaced_by same-tenant self-FK · aes_key_ref-sarmalı doğrulaması M1-08/M1-10'a devredildi |
| M1-06 | Migration 0005: transactions (append-only) & audit_log | **done** | `d91c609` · **iki denetçi ONAY** (kırmızı çizgi ihlali yok) · immutability kuşak+kemer (REVOKE UPDATE,DELETE + `tappa_forbid_mutation` trigger; superuser DISABLE-trigger sınırı kabul) · §4.6 nullable id + CHECK (flag/manuel/reject kaydedilebilir) · **`UNIQUE(tag_uid,ctr)` yok** · transaction_reviews 3 kısıt + çapraz-tenant review **yapısal** composite FK ile kapalı (X3/X4 kanıtı) · reviewer_id/entered_by FK M1-11'e ertelendi |
| M1-07 | internal/db: havuz ve tenant kapsamlı transaction | **done** | `f73972a` · üçüncü göz **1. turda** ONAY (3 negatif kontrolle: set_config true→false, rollback→commit, panic silme — üçü de testi kırmızıya döndürdü) · `WithTenant` `set_config(...,$1,true)` param-bağlı, çıplak SET yok · havuz unexported (yapısal kapalı) · uuid.Nil guard · 5 gerçek-Postgres -race test (aynı-backend sızıntı-yok kanıtı) · **imza sapması:** callback `pgx.Tx` (store M1-08'de) — kart düzeltildi · resolve erişimi + go.mod'a templ geri-dönüşü M1-08'e/M2'ye |
| M1-08 | İlk sqlc sorguları | **done** | `62b70a8` · **iki denetçi ONAY** · `make gen`/`build`/`dev` **yeşil** (planlı sqlc kırmızısı bitti) · 6 tenant-kapsamlı sorgu (hepsi açık tenant_id) · `AdvanceTagCounter` atomik CTE strict-`<` (canlı + 2-goroutine -race) · **resolve lookups ELLE** (`internal/db/resolve.go` — sqlc `RETURNS TABLE`'ı tipleyemedi; yalnız SECURITY DEFINER fonksiyon çağırır) · Q25(c) cidr[] override **gerekmedi** (pgx varsayılanı) · WithTenant pgx.Tx kaldı |
| M1-09 | RLS izolasyonu ve değişmezlik testleri | **done** | `a033c8a` · üçüncü göz ONAY — **non-vacuous 3 bağımsız yolla kanıtlandı** (RLS DISABLE, trigger DISABLE, kaynak mutasyonu → hepsi RED, geri alındı) · 7 vaka + 9 tablo · M0-03 kaçış yolları kapalı (ham SQL, pozitif kontrol, çalışma-anı rol) · `TestResolveColumns_MatchSchema` drift koruması · **2 sapma çözüldü:** x/text CVE yamalandı (`1554135`), redline R3/R5 `_test.go` muafiyeti + test sadeleştirildi (`<sonraki>`) |
| M1-10 | Seed verisi ve sabit ID'ler | **done** | `516be65` · üçüncü göz ONAY · KF 9 lokasyon + KM 5 departman, 36 çalışan, 12 tag · idempotent (2. koşu INSERT 0 0) · 12/12 sahte-etiketli anahtar (§4.7) · doküman IP cidr[] · Malta GPS min 783.6m · çift-uçlu vardiya · cross-tenant paylaşım 0 · ids.go 53 UUID+12 tag DB ile birebir · yalnız master veri (admin owner M1-11'e) |
| M1-11 | Migration 0006: admin kullanıcıları | **done** | `f416d45` · **iki denetçi ONAY** (kırmızı çizgi ihlali yok) · 3 tablo RLS beşlisi + REVOKE DELETE + composite same-tenant FK · **admin'de resolver YOK** (tenant login'de bilinir) · admin_sessions employee sessions'tan ayrı · Q03 bcrypt `password_hash text` (x/crypto M6-01'de) · seed admin owner (dev-only bcrypt) · rls_test +3 tablo (non-vacuous) · **back-FK'ler M6'ya ertelendi** (aşağı) |

### M2 — [SUN doğrulama](m2-sun.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M2-01 | ADR 0003: SDM modu ve anahtar yönetimi | **done** | `5a9cd2e` · üçüncü göz ONAY · **Q05 plain SDM + Q06 per-tag random** normatif · plain URL (`tag`/`ctr` big-endian/`cmac`) · KEK AES-256-GCM, `aes_key_ref`=nonce(12)‖ct(16)‖tag(16)=44B · **AAD=ham 7-byte UID v1'de ZORUNLU** (denetçi bulgusu: tappa_app UPDATE→sarmalı anahtar taşınabilir; sıfır maliyet pre-prod) · ctr-wrap fail-closed · AN12196/NT4H2421Gx ref |
| M2-02 | AES-CMAC (RFC 4493) | **done** | `2380baa` · üçüncü göz ONAY (RFC vektörleri **OpenSSL ile bağımsız yeniden hesaplandı**, mutasyonla non-vacuous) · kurum-içi `crypto/aes`, **dep yok** · 4 resmi vektör + K1/K2 + padding · **%100 kapsam** · kısaltma yok (M2-04) · `cmac(key,msg)([16]byte,error)` |
| M2-03 | SDM URL ayrıştırma | **done** | `ac51b20` · üçüncü göz ONAY · `Parse`→`Params{UID(kanonik BÜYÜK), UIDBytes, Ctr(big-endian), CMAC, Channel}`+`HasSUN()` · **mixed-case silent-zero-row tuzağı kapatıldı** (DB sondasıyla) · QR→sun_valid=false · fuzz 10.9M exec panik yok · §4.7 jenerik/sır-siz hata (mutasyonla) · yeni dep yok |
| M2-04 | Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma | **done** | `88c6036` · **iki denetçi ONAY** (1. tur RED: SV2 sayaç byte'ları URL'ye göre TERS'ti → yapısal düzeltildi, `sv2()` ham `ctrBytes` verbatim) · tek-indeksli 8-byte kısaltma · `ConstantTimeCompare` (R7) · golden bağımsız Python'la doğrulandı · %98.9 kapsam · **değer-endian M2-07'ye ertelendi** (ayrı eksen) |
| M2-05 | Anahtar sarmalama (KEK) | **done** | `0d23d30` · **iki denetçi ONAY** · `Wrap(kek,uid,key)`/`Unwrap(kek,uid,ref)`+`Zero()` AES-256-GCM · AAD=UID taşınabilirlik-koruması (uidA→uidB unwrap hata) · 44-byte düzen · **KEK parametre (cache yok)** · AES-256 zorlanıyor (downgrade önlenir) · düz-anahtar/KEK sızmaz (mutasyonla) · %96.1 kapsam |
| M2-06 | Atomik sayaç ilerletme ve eşzamanlılık testi | **done** | `2092796` · **iki denetçi ONAY** (§4.4 en kritik) · `sun.AdvanceCounter` M1-08 atomik CTE'sini kullanır (verify'dan ayrı) · **50-goroutine `-race` → tam 1 kazanan** (her iki denetçi kendi koştu) · **negatif kontrol yeniden üretildi** (TOCTOU→50 kazanan) · strict `<`, 0-satır→ErrReplay, gömülü eşik yok, R4 temiz · %96.3 kapsam |
| M2-07 | sun.Verify ve test vektörleri | **done** | `cd639f5` · **iki denetçi ONAY** · `Verify` tüm zinciri birleştirir (resolve→retired/lost→QR→unwrap+verifyMAC+Zero→**sonra** advance) · `Result` döner **verdict vermez** · vaka tablosu tam + N-goroutine tam-1 (`-race`) · sıra kanıtı (kötü CMAC→advance yok) · §4.7 no-leak mutasyonla · %96.5 kapsam · **self-consistent vektör** (gerçek çip M8-05'te) |

### M3 — [Policy motoru](m3-policy-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M3-01 | ADR 0004: policy motoru modeli | **done** | `01c7a8a` · üçüncü göz **1. turda** ONAY · `docs/adr/0004-policy-motoru-modeli.md` (413 satır, 0002/0003 iskeleti) · 7+3 içerik maddesi gerekçeli · **§5 satır 1–5↔guardrail, 6–7↔baseline** hem tablo hem düz metin (denetçi CLAUDE.md §5'i satır satır doğruladı) · 5 effect, 2 varsayılan (tap:*→review / authz→deny), guardrail sırası + 2 somut sömürü, ignore/redirect tenant'a kapalı, Y-K spesifik-ezer, 4 alternatif · biyometrik anahtar YOK (§4.1), §4 gevşetme yok · **2 bloklamayan gözlem M3-04'e devredildi** (kart `redirect` eksik, eval-time bilinmeyen operatör) |
| M3-02 | Policy şeması (append-only sürümler) | **done** | `4126e4c` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok) · migration 00007: policies + policy_versions (**append-only**) + policy_attachments, üçünde RLS beşlisi (birebir NULLIF USING+WITH CHECK, pg_policies'ten okundu) · §4.3 kuşak+kemer non-vacuous (trigger DISABLE→superuser UPDATE başarılı → koruma REVOKE değil paylaşılan `tappa_forbid_mutation` trigger) · **`layer` CHECK `guardrail`'i reddediyor** (23514 — guardrail DB'ye yazılamaz) · composite same-tenant FK çapraz-tenant'ı blokluyor (23503) · `tappa_app` rolsuper=f/rolbypassrls=f teyit · **2 sapma kabul:** `policies` DELETE REVOKE (§4.6 enabled durum alanı; planlı silme yolu yok), `created_by` FK-siz uuid (admin FK M6/M7'ye, M1-11 kalıbı) · rls_test.go +3 tablo non-vacuous · models.go make gen additive · make check/gen/audit yeşil |
| M3-03 | Belge modeli, ayrıştırma ve doğrulama | **done** | `555e1c5` · üçüncü göz **1. turda** ONAY (non-vacuous **2 mutasyonla** kanıtlandı: sys: no-op→test RED, documentEffect→true→test RED) · `internal/policy/{document,validate}.go`+testler, **%98.8 kapsam** · bilinmeyen effect/action/operatör/anahtar→hata (+ `DisallowUnknownFields` typo yakalama), sys: rezerve (case-insensitive, iki katman), ignore/redirect belgede reddedilir, nicel DoS sınırları (byte/ifade/action/resource/condition/IpInPrefix + `CheckTenantQuota` doc+version, sabitler tek yerde), bozuk JSON+fuzz (456K exec crasher yok), §4.7 hata değeri sızmıyor · saf paket (Evaluate M3-04'e bırakıldı) · ADR listeleri birebir (10 operatör/7 eylem/24 anahtar/5 effect) · **bounded-param wiring boş → M3-05'e devir** (aşağı) |
| M3-04 | Değerlendirici (koşullar, öncelik, açıklanabilirlik) | **done** | `de831e1` · üçüncü göz **1. turda** ONAY (non-vacuous **3 mutasyonla**: guardrail return kaldır→terminal RED, deny/review takas→restrictiveness RED, bilinmeyen-op matched=false kaldır→deny koşulsuzlaştı 4 test RED) · `internal/policy/{evaluate,conditions}.go`, **%97.9 kapsam** (evaluate.go %100) · saf `Evaluate(Set,Context) Decision` · guardrail sıralı+**terminal** (alt katman OnAnomaly çağırmıyor=hiç çalışmıyor kanıtı) · en-kısıtlayıcı-kazanır + spesifik-resource tie-break · varsayılan `tap:record`→review / diğer 6 (tap:approve dahil)→deny · **bilinmeyen-op deny'yi koşulsuzlaştırMIYOR** · eksik-anahtar≠false (StringNotEquals dahil) · determinizm 1000-koşu (map-sıra bağımsız) · anomaly injectable sink+slog fallback §4.7-temiz · **2 kart düzeltmesi** (redirect eksiği + tap:approve→deny ADR §3, denetçi doğruladı) · Context struct sapması gerekçeli · 2 bloklamayan not (Türkçe yorum→M3-05, default Layer=guardrail→M3-07) |
| M3-05 | Guardrail politikaları | **done** | `e51504b` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, **§4 en kritik**, kırmızı çizgi ihlali yok) · non-vacuous **3 mutasyonla** (deactivated'ı öne al→sıra+R8 leak RED; sun-invalid Match→false→R8 RED; config üst sınır kaldır→20000000 RED) · `internal/policy/guardrails.go` 10 `sys:*` guardrail TEK sıralı slice, kodda gömülü, devre-dışı API YOK · **R8 sıra** sun-invalid(3)<deactivated(7)<debounce(8) — üçü eşleşince sun-invalid kazanır + SecurityAlert BOŞ (sızıntı/push-seli/replay kapalı) · terminallik: geniş tenant allow guardrail deny'ini çeviremiyor · tenant-mismatch→redirect+kayıt-yok · person-debounce KİŞİ bazlı (nil gap→kayıt düşmez §4.6) · Context 4 tipli sunucu-alanı (belge sözlüğü dışı) · SecurityAlert sabit sözlük §4.7-temiz · **config aralık** GPS 25–1000/debounce 30–300 başlangıçta (20000000+GPS=5 reddedilir), guardrail+config tek kaynak · bounded-param 3 anahtar (occurredAtSkew dahil) · policy %98.2 · **N1/N2/N3 → M4/M5 devir** (aşağı) |
| M3-06 | Tappa Baseline yönetilen politikası | **done** | `a9b4dc6` · üçüncü göz **1. turda** ONAY (non-vacuous **3 mutasyonla**: no-evidence effect değiş→RED; base: rezerv no-op→RED; owner'dan policy:edit çıkar→owner default deny=**fail-closed lockout gerçek** kanıtı) · `internal/policy/baseline.go` 8 `base:*` tap ifadesi + **2 yetki ifadesi** (authz-owner=6 eylem, authz-manager=4 eylem alt kümesi) · fail-closed lockout önleniyor (owner policy:edit baseline allow — guardrail owner'da ateşlemez) · **base: rezerv** validate.go'ya eklendi (tenant layer, case-insensitive) · base:ctr-gap-review kaynak-kapsamlı + tenant override (specExact>specType) · guardrail dokunulmaz (allow-all tenant→retired/deactivated guardrail deny kazanır) · ignore/redirect yok · BaselineVersion + otomatik-güncelleme-yok · **DB yazma M3-06'da YOK** (kanonik kaynak, M7-03 materyalize) · rol modeli admin_users {owner,manager} teyit · baseline.go %100/policy %98.3 · **manager employee:deactivate: kullanıcı kararı = manager DA yapabilir** (`a6c41dd` followup, odaklı üçüncü göz ONAY; policy:edit owner-only kaldı) |
| M3-07 | Kararın kayda bağlanması | **done** | `1f144b7` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, §4.3 kırmızı çizgi ihlali yok) · migration 00008: transactions'a `policy_version_id`/`matched_sid`/`policy_layer`/`policy_context jsonb` (uygulanmış migration değişmedi) · **§4.6 kritik doğrulandı:** consistency CHECK Evaluate'in HER meşru kararını kabul eder (baseline `&vid` daima non-nil), yalnız wiring-bug keser (verdict CHECK precedent'i) — kayıt kaybı yok · §4.3 yeni sütunlar immutable (belt1 REVOKE sütun-seviyesi f + belt2 trigger DISABLE→superuser UPDATE başarılı kanıtı) · composite same-tenant FK policy_versions'a (23503 çapraz-tenant) + **ON DELETE RESTRICT** (cited version silinemez, delil zinciri) + policy_versions UNIQUE(id,tenant_id) hedefi · §4.7 policy_context mesafe/ham-koordinat değil · sqlc InsertTransaction+2 read additive (hepsi Transaction döner) · make check/gen/audit yeşil · **N4 → M5-05 devir** (Decision→sütun sadakati, aşağı) |
| M3-08 | Test seti ve gevşetilemezlik kanıtı | **done** | `c39ccae` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, guardrail bypass + sys: sızıntısı arandı, bulunamadı) · `internal/policy/{property,invariants}_test.go` (üretim kodu DEĞİŞMEDİ) · **özellik testi** `TestGuardrail_NoTenantPolicyCanLoosen` fixed-seed 2000 iter: hiçbir rastgele tenant/baseline politikası guardrail deny/ignore/redirect'i allow'a çeviremez · **non-vacuous** (iterasyon-başına guardrail-siz kontrol allow assert eder; üçüncü göz katman sırasını bozunca step-3 property RED) · security-auditor bağımsız 7-guardrail bypass sondası (en spesifik resource dahil hepsi tuttu) · **invariant testleri:** §4.6 kanıt-yok→review (2 yığın), §4.1 yüzey-kilidi (24 anahtar+8 Context alanı; key+field ekleme→RED; D1 denylist değil çünkü redline R1 _test.go tarar), guardrail-restrictive-only · §4.7 test hata mesajı yalnız anahtar-adı · kapsam %98.3 |
| M3-09 | ADR 0005: kabul edilen riskler | **done** | `0c0feb4` · üçüncü göz **1. turda** ONAY (12 kabul kriteri) · `docs/adr/0005-kabul-edilen-riskler.md` — 6 risk (buddy punching A4/Q19, sahte GPS A3, URL biriktirme A1/Q21, mekânda proxy Y-E, müdürün kimlik basması Y-D, plaket devri) her biri neden+tespit sinyali+görev+satış · **referanslanan 8 sid + 2 anahtar kodda GERÇEK** (denetçi grep'ledi: base:ctr-gap-review/gps-conflict-review/no-evidence-review, sys:tag-not-active/tenant-mismatch/tap-freshness/occurred-at-bound) · handoff §2 tutarlı (parmak izi=yalnız buddy punching) · mekânda-proxy uyarısı iki yönlü · append kuralı + §4.1 sınırı · "ileride bakarız" yok |

### M4 — [Tap karar motoru](m4-tap-motoru.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M4-01 | internal/geo: haversine ve yarıçap | **done** | `f791f91` · üçüncü göz **1. turda** ONAY · `internal/geo` saf paket (yalnız `math`) — `Point{Lat,Lng}`, `Distance` (haversine, R=6371008.8, **atan2** → acos-NaN tuzağı yapısal yok), `WithinRadius(a,b,radiusM)` **strict `<`** (§5 satır 6 "GPS < 150 m" ile hizalı, 150 m DIŞARIDA) · yarıçap **parametre** (config besler, gömülü değil) · **denetçi mesafeleri BAĞIMSIZ yeniden hesapladı** (783.557/1115.594/0/π·R byte-identical) · lat/lng-swap + %100 kapsam mutasyonla RED · §4.7 koordinat loglanmıyor (config/policy import yok, döngü yok) |
| M4-02 | Karar girdi/çıktı tipleri | **done** | `860fcd8` · üçüncü göz **1. turda** ONAY · `internal/domain/tap/{types,decide}.go` — `Input` (14 alan) + `Decision` (9 alan) karta birebir + `Decide(Input) Decision` imzası (gövde M4-03 **panic-stub**, zero-value §4.6 sessiz-onay riski yok) · **saf** (kendi import'ları `net/netip,time,geo,uuid`; store/db/sun/sql/http/pgx KODDA yok; `time.Now()` çağrısı yok; math/rand+database/sql/driver yalnız uuid'den, policy ile birebir) · enum'lar typed (DB CHECK sözlükleriyle birebir) · **`Employee` pointer (§5.3 nil=oturum yok) + Status (§5.4 deactivated) ayrı** · tap kendi `SUNResult`'ı (sun.Result db/store sürüklüyor) · sapma: `Employee.ActivatedAt` (Practice sunucu-türetim kaynağı, §5/M4-06 exploit önler) |
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

**Özet:** 82 görev · done 36 · wip 0 · blocked 0 · skipped 1 · todo 45 · **M0+M1+M2+M3 TAMAM · M4 devam (M4-01,M4-02 done, 2/7) · sıradaki M4-03**

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

### 2026-07-26 (4. oturum, compact sonrası) — **M4-02 done** (karar girdi/çıktı tipleri)

**M4-02 done — üçüncü göz 1. turda ONAY.** `860fcd8`: `internal/domain/tap/{types,decide}.go`. `Input`
(14 alan) + `Decision` (9 alan) karta birebir; `Decide(Input) Decision` imzası sabit, gövde M4-03
**panic-stub** (zero-value dönmez → §4.6 sessiz-onay tuzağı yok). **Saflık kanıtlandı:** paketin kendi
import'ları `net/netip,time,internal/geo,uuid` — store/db/sun/database-sql/http/pgx KODDA yok, `time.Now()`
çağrısı yok; `math/rand`+`database/sql/driver` yalnız uuid transitifi (policy ile birebir aynı). Enum'lar
typed (migration CHECK sözlükleriyle birebir: nfc/qr/manual, ok/flag/reject/ignored, in/out, active/retired/
lost, invited/active/deactivated). **`Employee` pointer (§5.3 nil=oturum yok) + `Status` (§5.4 deactivated)
ayrı** → iki farklı karar mümkün. tap kendi `SUNResult`'ı (sun.Result db/store/pgx sürüklediği için import
edilmedi; M5 map eder). Sapma (meşru): `Employee.ActivatedAt` — Practice **sunucu-türetim** kaynağı
(Input'ta client practice bool'u yok → M4-06 exploit'i önlenir).

**Sırada:** M4-03 (Decide gövdesi — bağlam kur, policy.Evaluate çağır, effect→verdict). Açık nokta: Decide
policy.Set'i nasıl alacak (M4-02 imzası Set içermiyor) — M4-03 çözer + kartı düzeltir.

### 2026-07-26 (4. oturum, compact sonrası) — **M4-01 done** (internal/geo) · M4 başladı

**M4-01 done — üçüncü göz 1. turda ONAY.** `f791f91`: `internal/geo` saf paket (yalnız `math` import).
`Point{Lat,Lng}`, `Distance` (haversine, R=6371008.8 IUGG ortalama, **atan2** formülü → acos domain-NaN
tuzağı yapısal olarak yok), `WithinRadius(a,b,radiusM)` yarıçap **parametre** (config besler; §5 satır 6
"GPS < 150 m" gereği **strict `<`** → tam 150 m dışarıda). Kullanıcı M3 sonrası "M4'e devam et" dedi.

**Denetçi bilinen mesafeleri BAĞIMSIZ yeniden hesapladı** (kendi Python haversine, R aynı): St Julians→
Paceville 783.5570309985226 m, Hamrun→Msida 1115.5938858223842 m, 0 m, antipot π·R — hepsi byte-identical
(iç-tutarlı golden değil dış hesap). lat/lng-swap direnci + %100 kapsam **mutasyonla** RED kanıtlandı
(swap→761.77; Distance sabit→testler RED). §4.7 koordinat loglanmıyor; geo config/policy import etmiyor (saf).

**Sırada:** M4-02 (karar girdi/çıktı tipleri — Input/Decision struct, saf imza, Employee==nil ≠ deactivated).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-09 done · 🏁 M3 KİLOMETRE TAŞI TAMAM (9/9)**

**M3-09 done — üçüncü göz 1. turda ONAY** (12 kabul kriteri). `0c0feb4`: `docs/adr/0005-kabul-edilen-riskler.md`
— policy motorunun + dört kanıtın çözemediği 6 riski yazılı kabul (buddy punching, sahte GPS, URL biriktirme,
mekânda proxy, müdürün kimlik basması, plaket devri); her biri neden+tespit sinyali+görev+satış. Denetçi
referanslanan 8 sid + 2 anahtarı kodda grep'leyip GERÇEK olduğunu, handoff §2 tutarlılığını doğruladı.

**🏁 M3 TAMAM (9/9):** ADR 0004 (motor modeli) · policy şeması (00007, append-only) · belge modeli+doğrulama ·
değerlendirici (saf, guardrail terminal, deterministik) · **10 guardrail** (§4, sıra normatif, R8 sömürüsü
kapalı) · Tappa baseline (8 tap + 2 authz ifadesi, fail-closed lockout çözüldü) · kararın kayda bağlanması
(00008, delil zinciri) · **gevşetilemezlik özellik testi** (hiçbir tenant politikası guardrail'i allow'a
çeviremez) · ADR 0005 (kabul edilen riskler). Her görev builder→üçüncü göz; kırmızı çizgi görevlerinde
(M3-02/05/07 + M3-08) **iki denetçi** (+ tappa-security-auditor). policy kapsamı %98.3. Kullanıcı kararı:
manager employee:deactivate (followup `a6c41dd`). **Tüm kripto/DB/policy stdlib + mevcut dep — yeni dep yok.**

**M4/M5'e devreden (ŞU AN'da):** N1 tap:sunValid set · N2 channel sunucu-türetimi · N3 debounce Params'a bağla ·
N4 Decision→sütun sadakati (M5-05) · ErrUnknownTag güvenlik olayı logla. **Bekleyen kullanıcı kararı: yok.**

**Sırada:** M4-01 (internal/geo — yeni kilometre taşı; M4 kartını baştan oku). **Milestone sınırı — kullanıcı
inceleme molası verebilir.**

### 2026-07-26 (4. oturum, compact sonrası) — **M3-08 done** (gevşetilemezlik kanıtı)

**M3-08 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor; guardrail bypass + sys: sızıntısı
özellikle arandı, bulunamadı). `c39ccae`: `internal/policy/{property,invariants}_test.go` — **üretim kodu
DEĞİŞMEDİ**. Merkezî **özellik testi**: fixed-seed (20260726) 2000 iter, hiçbir rastgele tenant/baseline
politikası guardrail deny/ignore/redirect'i allow'a çeviremez. **Non-vacuous** iki yolla: (1) iterasyon-başına
guardrail-siz kontrol allow assert eder (üreteç gerçek düşman belge üretiyor), (2) üçüncü göz katman sırasını
bozunca **step-3 property assertion'ında** RED (sanity guard'da değil). security-auditor bağımsız 7-guardrail
bypass sondası koştu (retired/lost/sun-invalid/deactivated/tenant-mismatch/no-session/person-debounce'a karşı
allow+resource `*`, en spesifik location/rusty-bar dahil) → hepsi guardrail effect'inde kaldı.

**Invariant testleri (guardrail değil, ayrı):** §4.6 kanıt-yok→review (2 yığın: tam baseline + hiç politika);
§4.1 **yüzey-kilidi** (24 anahtar + 8 Context alanı birebir; üçüncü göz key ekle→25vs24 RED, field ekle→9vs8 RED).
**D1 sapması:** §4.1 testi başta biyometrik-terim denylist'iydi ama redline **R1 biyometri tarayıcısı `_test.go`'yu
da tarar** → FAIL etti; R1'i düzeltmek (tracked araç) make check git-diff kapısını kırardı (commit yasak) → test
yapısal yüzey-kilidine çevrildi (yasak terim adı geçmez, kart gereğini karşılar). Kapsam %98.3.

**Sırada:** M3-09 (ADR 0005 kabul edilen riskler — M3'ün SON görevi; ADR, kod yok; tek genel üçüncü göz).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-07 done** (kararın kayda bağlanması)

**M3-07 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, §4.3, kırmızı çizgi ihlali yok).
`1f144b7`: migration 00008 — transactions'a `policy_version_id`/`matched_sid`/`policy_layer`/`policy_context
jsonb`. Yapıcı = agent `tappa-db-migrator`. İki denetçi **sıralı** koşuldu (DB mutasyon sondası).

**§4.6 kayıt-kaybı riski (en kritik) — iki denetçi de temiz buldu:** yapıcı bir consistency CHECK ekledi
(version NULL ⟺ guardrail/default; her decided satır sid taşır). Risk: CHECK meşru bir tap'i reddederse kayıt
kaybolur. Kanıt: Evaluate baseline/tenant kararında `PolicyVersionID`'yi **daima non-nil** (`&vid`) döndürür
(evaluate.go:325-333) → CHECK meşru hiçbir kararı reddetmez; yalnız Evaluate'in asla üretemeyeceği wiring-bug
şekillerini keser (00005 verdict/channel CHECK precedent'i). Her denetçi Evaluate'in 6 meşru şeklini canlı
INSERT'le CHECK'ten geçirdi. **§4.3:** yeni sütunlar belt1 (REVOKE sütun-seviyesi f) + belt2 (trigger DISABLE→
superuser UPDATE başarılı kanıtı). **Delil zinciri:** FK `ON DELETE RESTRICT` — cited version append-only
trigger DISABLE'ken bile silinemedi (RESTRICT'in FK olduğu kanıtı). §4.5 composite FK (23503) + RLS t/t.
§4.7 policy_context mesafe (tap:gpsDistanceM), ham koordinat değil. sqlc InsertTransaction additive.

**Bloklamayan → M5-05 devir (N4, ŞU AN'a yazıldı):** CHECK+FK'nin §4.6 güvenliği M5-05'in Decision→sütun
sadakatine bağlı (baseline'ı uuid.Nil version ile yüklerse FK 23503→kayıt kaybı; Evaluate bugün üretmez).

**Sırada:** M3-08 (test seti + gevşetilemezlik kanıtı — özellik testi: hiçbir tenant politikası guardrail'i
allow'a çeviremez; guardrail sıra testi; invariant testleri §4.6/§4.1; kapsam %90+; iki denetçi).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-06 done** (Tappa Baseline yönetilen politikası)

**M3-06 done — üçüncü göz 1. turda ONAY.** `a9b4dc6`: `internal/policy/baseline.go` — kanonik Tappa
baseline. **8 `base:*` tap ifadesi** (§5.6-7 + boşluklar: qr-requires-ip Q15, gps-only-allow Q16,
cross-location-note Q17, queued-window Y7, ctr-gap-review Q21, gps-conflict-review Y-E) + **2 yetki ifadesi**.

**Fail-closed lockout çözümü (kartın kritik kabul kriteri):** ADR §3 authz→deny varsayılanı yeni tenant'ta
herkesi panelden kilitlerdi. İnce nokta: `sys:policy-edit-owner-only` guardrail'i yalnız non-owner'ı reddeder,
owner'da ATEŞLEMEZ → baseline allow olmasa owner kendi policy:edit'inde default deny'ye takılırdı. Çözüm:
`base:authz-owner` (owner→6 eylem incl policy:edit), `base:authz-manager` (manager→report:export/tap:approve/
record:manual/record:review; employee:deactivate + policy:edit HARİÇ). Roller admin_users {owner,manager}
CHECK'ini yansıtır. Denetçi bunu mutasyonla kanıtladı (owner'dan policy:edit çıkar→owner default deny).

**Diğer:** `base:` ad alanı rezervi validate.go'ya eklendi (tenant layer, case-insensitive — sys: kalıbı);
base:ctr-gap-review kaynak-kapsamlı (yoğun şube override edebilir, Q21); guardrail dokunulmaz (allow-all tenant
altında retired/deactivated→guardrail deny kazanır); ignore/redirect yok; BaselineVersion + otomatik-güncelleme-
yok; **M3-06'da DB yazma YOK** (kanonik kaynak kod'da, M7-03 tenant başına materyalize eder). baseline.go %100.

**Kullanıcı kararı (2026-07-26):** manager `employee:deactivate` **yapabilir** ("Manager da deaktive edebilsin").
Followup `a6c41dd`: `base:authz-manager`'a `employee:deactivate` eklendi (allow), testler güncellendi; `policy:edit`
manager'da HÂLÂ yok (guardrail sys:policy-edit-owner-only terminal + grant'ta yok). Odaklı üçüncü göz ONAY
(non-vacuous: action'ı çıkar→test RED; owner/roleless değişmedi; guardrail etkilenmedi). policy %98.3.

**Sırada:** M3-07 (kararın kayda bağlanması — transactions'a policy_version_id/matched_sid/policy_layer/
policy_context, EK migration 00008, agent tappa-db-migrator + iki denetçi). **Şu an arka planda WIP.**

### 2026-07-26 (4. oturum, compact sonrası) — **M3-05 done** (guardrail politikaları — §4 EN KRİTİK)

**M3-05 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok). `e51504b`:
`internal/policy/guardrails.go` — 10 `sys:*` guardrail TEK sıralı slice, kodda gömülü, DB'de değil, **devre
dışı bırakma API'si YOK**. İki denetçi **sıralı** koşuldu (M3-02 dersi). policy kapsamı **%98.2**.

**R8 sıra sömürüsü kapalı — mutasyonla kanıtlandı:** sun-invalid(3) < deactivated(7) < debounce(8). Üçünü
eşleştiren bağlamda sun-invalid kazanır, deny, **SecurityAlert BOŞ** → forge'lu SUN deaktivasyon durumunu
sızdıramaz, müdüre push seli yollayamaz, replay `ignore`'a yutulmaz. `TestGuardrails_OrderIsLoadBearing`
non-vacuous (yanlış sırada sızıntı geri geliyor); üçüncü göz deactivated'ı öne taşıyıp RED gördü.

**Tasarım (iki denetçi kabul):** guardrail girdileri 24 belge anahtarı dışı → **tipli Context alanları**
(SessionTenantID/TagTenantID/SecondsSincePersonLastTap/Reviewer+SubjectID), sunucu-türetimi, belge sözlüğü
DIŞI (tenant set edemez), additive (M3-04 testleri geçer), nil=güvenli (§4.6 kayıt düşmez). Güvenlik uyarısı
= `Decision.SecurityAlert` sabit sözlük (lost-tag-tapped/deactivated-employee-tapped), yalnız guardrail
ateşleyince, §4.7-temiz (değer/GPS/sır taşımaz). **config aralık kontrolü:** GPS 25–1000/debounce 30–300
başlangıçta (TAPPA_GPS_RADIUS_M=20000000 + GPS=5 artık reddedilir — proof-of-place tek env ile kapatılamaz),
guardrail+config **tek sabit kaynağı**. **Bounded-param 3 anahtar** dolduruldu (M3-03 kancası; occurredAtSkew
dahil — M3-03'te kaçırılan). evaluate.go:169 Türkçe yorum İngilizce'ye çevrildi.

**3 bloklamayan not → M4/M5 devir** (guardrail girdi sözleşmesi, ŞU AN'a yazıldı): N1 M5 her NFC tap'te
`tap:sunValid` set etmeli; N2 `channel` ctr/cmac'ten sunucu-türetimi (istemci `qr` diyip SUN atlayamamalı);
N3 `TAPPA_DEBOUNCE_SECONDS` henüz `policy.Params`'a bağlanmadı (değer akışı M4/M5).

**Sırada:** M3-06 (Tappa Baseline yönetilen politikası — 8 `base:*` ifade).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-04 done** (değerlendirici — motorun doğruluk çekirdeği)

**M3-04 done — üçüncü göz 1. turda ONAY.** `de831e1`: `internal/policy/{evaluate,conditions}.go`, **%97.9
kapsam** (evaluate.go %100). Saf `Evaluate(Set,Context) Decision` — M3-03 tipleri üstünde. Guardrail'ler
**sıralı+terminal** (kod-inşa closure, sys: + ignore/redirect serbest; M3-05 `Set.Guardrails`'i doldurur);
baseline+tenant en-kısıtlayıcı-kazanır + spesifik-resource tie-break; varsayılan tap:record→review / diğer
6→deny; bilinmeyen-op eval→ifade inert (deny koşulsuzlaşMAZ) + injectable anomaly sink (nil→slog); eksik
anahtar≠false; deterministik.

**Denetçi non-vacuous'u 3 mutasyonla kanıtladı** (terminal, restrictiveness, bilinmeyen-op) + kendi
adversaryel testleriyle terminalliği yan-etki-sayımıyla (OnAnomaly calls==0), determinizmi 1000-koşuyla,
§4.7 anomaly hijyenini kötü bağlamla doğruladı. **Kartın iki düzeltmesini onayladı:** denetçi ADR §3'ü
kendi okudu → `tap:approve` gerçekten fail-closed deny (yalnız tap:record→review); `Decision` yorumuna
`redirect` eklendi.

**Tasarım kararları (kabul):** `Context` struct (Action/Resources map anahtarı olamaz); "log" = injectable
`Set.OnAnomaly` + slog fallback (saf kalır, sinyal kaybolmaz, §4.7 yalnız sözlük); default kararı
Layer=guardrail + sid="default" (dördüncü Layer uydurulmadı). **2 bloklamayan devir:** evaluate.go:169
Türkçe yorum→M3-05/M3-08; default-Layer ayrımı→M3-07 (matched_sid ile). ŞU AN'a yazıldı.

**Sırada:** M3-05 (guardrail politikaları — §4 EN KRİTİK, iki denetçi + R8 sıra kontrolü + bounded-param
kancasını doldurma + config aralık kontrolü).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-03 done** (belge modeli + doğrulama)

**M3-03 done — üçüncü göz 1. turda ONAY.** `555e1c5`: `internal/policy/{document,validate}.go` + testler,
**%98.8 kapsam**. Belge modeli ADR 0004'e sadık (5 effect · 10 operatör · 24 anahtar · 7 eylem — denetçi
saydı, birebir). `Parse` byte-cap + strict JSON (`DisallowUnknownFields` typo'lu alanı yakalar); `Validate`
yazma-anı kapı: bilinmeyen effect/action/operatör/anahtar → **hata** (sessiz yok sayma yok — en tehlikeli
başarısızlık); ignore/redirect belgede reddedilir (yalnız kod-guardrail üretir); `sys:` rezerve
(case-insensitive, iki katman); nicel DoS sınırları (Evaluate her tap'te, tek VPS). §4.7: hata mesajı
belge değerini echo etmiyor. Saf paket — Evaluate M3-04'e.

**Denetçi non-vacuous'u 2 mutasyonla kanıtladı** (sys: kontrolü no-op → test RED; documentEffect→true →
test RED; geri alındı, sha teyit). Kendi kötü belgeleriyle 4 bilinmeyen-kategori + sys: 4 varyant + typo
alanı + §4.7 (`424242`/`SECRET`/`10.9.8.7` mesajda yok) reddini üretti; FuzzParse'ı kendi koştu.

**Devir → M3-05 (bloklamayan):** bounded-param mekanizması test edilmiş ama `DefaultLimits().BoundedParams`
BOŞ → §11 koruması üretimde fiilen yok. Denetçi düzeltmesi: **üç** eşlenebilir anahtar (gpsDistanceM,
pageAgeSeconds, **occurredAtSkewSeconds**) + debounce (config-only). M3-05 üçünü + config sınırlarını
doldurmalı (ŞU AN'a yazıldı).

**Sırada:** M3-04 (değerlendirici — saf `Evaluate`, guardrail sıralı + terminal, en-kısıtlayıcı-kazanır,
spesifik-resource-ezer, deterministik). M3-04 başında kart `Decision` yorumundaki `redirect` eksiğini düzelt.

### 2026-07-26 (4. oturum, compact sonrası) — **M3-02 done** (policy şeması, migration 00007)

**M3-02 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi ihlali yok).
`4126e4c`: migration 00007 — `policies` + `policy_versions` (**append-only**) + `policy_attachments`,
üçünde RLS beşlisi (birebir NULLIF USING+WITH CHECK). Yapıcı = agent `tappa-db-migrator`. **İki denetçi
SIRALI koşuldu** (paylaşılan Postgres'te mutasyon sondası çakışmasın diye); üçüncü göz DB'yi migration 7
temiz bıraktı, security-auditor write-sondalarını rollback tx'te yaptı.

**§4.3 kuşak+kemer non-vacuous kanıtı:** trigger DISABLE edilince superuser UPDATE **başarılı** oldu →
korumanın REVOKE (superuser atlar) değil paylaşılan `tappa_forbid_mutation` **trigger** olduğu kanıtlandı;
restore edildi. **Guardrail DB'ye yazılamıyor:** `layer` CHECK `guardrail`'i reddediyor (23514) → bir SQL
erişimi kırmızı çizgiyi kapatamaz (§4 varlık sebebi). Composite same-tenant FK çapraz-tenant link'i
blokluyor (23503); `tappa_app` rolsuper=f/rolbypassrls=f (izolasyon kökü).

**2 tasarım sapması — iki denetçi de kabul etti:** (1) `policies` DELETE **REVOKE**'lu (§4.6: silme yerine
`enabled` durum alanı; planlı silme yolu yok — seed/reset owner ile DROP kullanıyor); (2) `created_by uuid`
FK-siz (baseline'ı sistem yazar→NULL; admin FK M6/M7'ye ertelendi, M1-11 kalıbı). `policy_attachments`
tam mutable (attachment karar geçmişi taşımaz; geçmiş `transactions.policy_version_id`+`policy_context`'te).

**Ders (agent-brief'e):** paylaşılan canlı Postgres'e karşı **iki denetçi sıralı** koşulmalı, ya da write
sondaları rollback tx'inde yapılmalı — eşzamanlı RLS/trigger DISABLE + migrate down/up birbirini bozar.

**Sırada:** M3-03 (belge modeli + ayrıştırma + doğrulama — `internal/policy`, saf Go, DB yok).

### 2026-07-26 (4. oturum, compact sonrası) — **M3-01 done · M3 başladı**

**M3-01 done — üçüncü göz 1. turda ONAY.** `01c7a8a`: `docs/adr/0004-policy-motoru-modeli.md`
(ADR 0004, policy motoru modeli). Compact noktasından temiz devralındı; ortam sağlıklı (ağaç temiz,
Go 1.26.5, tappa-db healthy). ADR, M3 kartında zaten normatif tasarlanmış modeli **gerekçeleriyle**
karara bağlar: IAM benzeri belge yapısı · 5 effect (allow|review|deny|ignore|redirect) · **2 farklı
varsayılan** (tap:*→review fail-to-review §4.6 / authz eylemleri→deny fail-closed) · 3 katman
(guardrail/baseline/tenant) · guardrail sırası NORMATİF + 2 somut sömürü (sun-invalid<employee-
deactivated → bilgi sızıntısı+push seli; sun-invalid<person-debounce → replay penceresi) · ignore/
redirect tenant'a kapalı · Y-K spesifik-kaynak-ezer · append-only sürümleme · açıklanabilirlik ·
sınırlı parametre · 4 reddedilen alternatif (if/ayar tablosu/Rego-OPA/CEL).

**Denetim (bağımsız):** üçüncü göz CLAUDE.md **§5'i satır satır** doğruladı — 1–5↔guardrail,
6–7↔baseline eşlemesi (hem tablo hem düz metin) doğru; effect eşlemesi tutarlı; operatör/anahtar/
eylem/kaynak listeleri spec ile **birebir** (10/24/7/4); biyometrik bağlam anahtarı **YOK** (§4.1);
§4 gevşetme yok; M3-05'in 10 guardrail sid'i + M3-06 baseline sid'leri kartla uyumlu; M3-03/M3-04
bu ADR'den türetilebilir. Kaçış yolu (başlık-only içi-boş) denendi, ADR düşmemiş — her madde gerekçeli.

**2 bloklamayan gözlem → M3-04'e devredildi** (ŞU AN'da): (1) M3-04 kartındaki `Decision` struct
yorumu `redirect`'i atlıyor — kart düzeltilmeli; (2) ADR eval-time bilinmeyen operatör davranışını
yazmıyor, M3-04 kartı yazıyor (çelişki yok). ADR görevi olduğu için tek genel üçüncü göz (M1-01/
M2-01 precedent'i; dual-audit kod/migration görevlerine mahsus).

**Sırada:** M3-02 (policy şeması — 3 tablo, append-only sürümler, agent `tappa-db-migrator`).

### 2026-07-26 (3. oturum, devam) — **M2-07 done · M2 KİLOMETRE TAŞI TAMAMLANDI** ✅

**M2-07 done — iki denetçi ONAY.** `cd639f5`: `sun.Verify` tüm SUN zincirini birleştirir
(parse→resolve→retired/lost→QR→unwrap+verifyMAC+Zero→**doğrula SONRA** advance) ve `Result` döner
(**verdict VERMEZ** — o M4). Vaka tablosunun tamamı + 50-goroutine tam-1 canlı `-race`; sıra kanıtı
(kötü CMAC→advance yok, last_ctr sabit); §4.7 no-leak mutasyonla; %96.5 kapsam. Sapmalar (unknown
UID→ErrUnknownTag, unwrap hatası→error) gerekçeli/kabul.

**🏁 M2 TAMAM (7/7):** RFC 4493 AES-CMAC (dış vektör) · SDM URL ayrıştırma (mixed-case kanonik) ·
session key + tek-indeksli 8-byte MAC + ConstantTimeCompare (RED yakalandı: SV2 byte-reversal
düzeltildi) · KEK sarmalama (AAD=UID, cache yok) · atomik ctr (50-goroutine tam-1 + negatif kontrol) ·
sun.Verify entegrasyonu. Tüm kripto stdlib (`crypto/aes`), yeni dep yok.

**⚠️ DEVAM EDEN GAP → M8 pilot / M4:**
- **Gerçek çip vektörü YOK:** tüm SUN zinciri self-consistent (sahte vektör) doğrulandı; RFC 4493
  CMAC dış-vektörle sabit ama SV2 ctr **mutlak** endian'ı + zincirin gerçek NTAG 424'e karşı
  doğruluğu **dış-doğrulanmadı**. **M8-05 pilot öncesi gerçek çip SUN URL'si uçtan uca test edilmeli**
  (üretim etiketleri encode edilmeden).
- **M4/M5:** `sun.Verify` `ErrUnknownTag` döner — decision/handler bunu **yutmamalı**, global güvenlik
  olayı olarak loglamalı (bilinmeyen uid'in tenant'ı yok → transactions kaydı kurulamaz; kayıt kararı M5).

**Sırada:** M3-01 (ADR 0004: policy motoru modeli).

### 2026-07-26 (3. oturum, devam) — **M2-06 done** (atomik sayaç, §4.4 en kritik)

**M2-06 done — iki denetçi ONAY** (projenin en güçlü doğrulaması). `2092796`: `sun.AdvanceCounter`
M1-08 atomik CTE'sini kullanır (verify'dan ayrı). **50-goroutine `-race` → tam 1 kazanan**; her
iki denetçi **kendi koştu** (3750+ yarış) ve **negatif kontrolü yeniden üretti** (sorguyu TOCTOU'ya
çevirince değiştirilmemiş test → 50 kazanan → harness gerçekten yarışıyor + atomiklik gerçek
koruma). tappa-security-auditor bağımsız psql sondasıyla EvalPlanQual re-fetch'i doğruladı. strict
`<`, 0-satır→ErrReplay, gömülü eşik yok (gap veri olarak döner), R4 temiz, %96.3 kapsam.

**⚠️ Devam eden gap (M2-07 + M8):** Tüm SUN zinciri şu ana kadar **self-consistent** (sahte
vektörler) doğrulandı — CMAC (RFC 4493 dış vektör ✔) hariç, SV2 ctr **mutlak** byte-sırası/endian
ve tüm zincirin GERÇEK bir NTAG 424 çipine karşı doğruluğu **henüz dış-doğrulanmadı** (skill/ADR'de
gerçek çip vektörü yok). M2-07 bunu flag'ler; **M8 pilot öncesi gerçek bir çipin SUN URL'si uçtan
uca doğrulanmalı** (üretim etiketleri encode edilmeden — M8-05 runbook).

**Sırada:** M2-07 (sun.Verify + vektör tablosu) — M2'nin son görevi.

### 2026-07-26 (3. oturum, devam) — **M2-05 done** (KEK sarmalama)

**M2-05 done — iki denetçi ONAY.** `0d23d30`: `internal/sun/keys.go` Wrap/Unwrap + Zero,
AES-256-GCM + AAD=ham 7-byte UID. **KEK parametre** (paket-seviyesi KEK state yok — cache tuzağı
kapalı); açılan anahtar uzun-ömürlü yapıya kopyalanmıyor. AAD=UID taşınabilirlik-koruması (uidA
sarıp uidB açma→hata) kendi sondasıyla kanıtlandı; **düz-anahtar/KEK sızmaz** mutasyonla (KEK
enjeksiyonu→leak testi RED); AES-256 zorlanıyor (16/24-byte KEK reddi→downgrade önlenir); 44-byte
düzen, uzunluk-KEK'ten-önce. %96.1 kapsam, redline-R7 temiz, yeni dep yok. TAPPA_TAG_KEK config'te
zaten 32-byte doğrulanıyor.

**Sırada:** M2-06 (atomik sayaç + N-goroutine eşzamanlılık — §4.4 en kritik).

### 2026-07-26 (3. oturum, devam) — **M2-04 done** (session key + truncated MAC) · RED yakalandı

**M2-04 done — iki denetçi, 2 tur.** `88c6036`: SDM doğrulama çekirdeği (SV2→K_session→boş MAC→
tek-indeksli 8-byte kısaltma→`ConstantTimeCompare`). **1. tur RED — genel üçüncü göz bloklayan
DOĞRULUK hatası buldu** (güvenlik denetçisi §4.7 merceğiyle kaçırmıştı): SV2 sayaç byte'ları
URL'ye göre **TERS**ti (M2-03 BE-parse + M2-04 LE-serialize) → palindromik-olmayan her gerçek
tap reddedilirdi, M2-07'de patlardı. **Yapısal düzeltme:** `sv2()` ham `ctrBytes`'ı verbatim
kullanır (`params.CtrBytes` eklendi); `Ctr uint32` yalnız M2-06 replay değeri için ayrı eksen.
2. tur ONAY: bağımsız Python CMAC + non-vacuous mutasyon (ctr terslenince test FAIL) ile SV2=URL
verbatim kanıtlandı; golden `d22ca9ef3a6b3b5d`. %98.9 kapsam.

**Ders:** iç-tutarlı golden byte-sırası hatasını yakalamaz; §4.7-odaklı denetçi doğruluk hatasını
görmeyebilir → bağımsız genel üçüncü göz şart oldu. **Değer-endian (M2-06 monotonik) M2-07 gerçek
vektörüne ertelendi** — reversal ekseninden ayrı.

**Sırada:** M2-05 (KEK sarmalama, Wrap/Unwrap AAD=UID).

### 2026-07-26 (3. oturum, devam) — **M2-03 done** (SDM URL ayrıştırma)

**M2-03 done — üçüncü göz ONAY.** `ac51b20`: `internal/sun/params.go`. Parse → `Params`
(UID kanonik BÜYÜK + UIDBytes ham 7 + Ctr big-endian + CMAC ham 8 + Channel/HasSUN).
**Mixed-case silent-zero-row tuzağı kapatıldı** (seed BÜYÜK saklıyor → parser uppercase kanonik;
denetçi DB sondasıyla doğruladı: `04AC…`→1 satır, `04ac…`/`04Ac…`→0). QR (ctr/cmac yok)→
sun_valid=false, hata değil; tam biri varsa hata. Big-endian ctr. §4.7 jenerik+sır-siz hata
(mutasyonla kanıtlandı). Fuzz 10.9M exec panik yok. Yeni dep yok.

**Sırada:** M2-04 (session key + tek-indeksli 8-byte MAC + ConstantTimeCompare) — kripto çekirdeği;
SV2 ctr byte sırası bilinen-cevap vektörüyle sabitlenmeli.

### 2026-07-26 (3. oturum, devam) — **M2-02 done** (AES-CMAC)

**M2-02 done — üçüncü göz ONAY.** `2380baa`: kurum-içi RFC 4493 AES-CMAC (`crypto/aes`, yeni
dep yok — ADR 0001). Dört resmi §4 vektörü PASS, K1/K2/dbl/padding testleri, **%100 kapsam**,
kısaltma yok (M2-04). Denetçi RFC vektörlerini **OpenSSL ile bağımsız yeniden hesapladı** + bayt
mutasyonuyla non-vacuous kanıtladı. API: `cmac(key, msg) ([16]byte, error)` (M2-04 kullanacak).
İki sapma (kabul): error dönüşü (§7 aes hatasını yutmaz), hata mesajı R7 "cmac" kelimesinden
kaçınacak biçimde yeniden yazıldı (daha açıklayıcı).

**Sırada:** M2-03 (SDM URL ayrıştırma).

### 2026-07-26 (3. oturum, devam) — **M2-01 done** (ADR 0003) · M2 başladı

**M2-01 done — üçüncü göz ONAY.** `5a9cd2e`: ADR 0003 (SDM modu + anahtar yönetimi). Kullanıcı
kararları: **Q05 = plain SDM** (`e81da68`), **Q06 = plaket-başına rastgele anahtar**. Normatif:
plain URL (`tag`/`ctr` big-endian/`cmac`), per-tag random AES-128, KEK AES-256-GCM
(`aes_key_ref`=nonce(12)‖ct(16)‖tag(16)=44B), MAC-input boş, ctr-wrap fail-closed, AN12196 ref.

**Denetçi bulgusu → uygulandı:** ADR AAD=UID'yi "ileri sertleştirme"ye erteliyordu; denetçi bunun
**ters** olduğunu gösterdi (pre-production, hiçbir tag sarılmadı → AAD şimdi bedava; tappa_app
`tags` UPDATE'e sahip → AAD'siz sarmalı anahtar satırlar arası taşınabilir). **AAD=ham 7-byte UID
v1'de ZORUNLU** yapıldı (Wrap(uid,key)/Unwrap(uid,ref)); aes_key_ref değişmedi.

**Sırada:** M2-02 (AES-CMAC RFC 4493, kurum-içi, dep yok).

### 2026-07-25/26 (3. oturum, devam) — **M1-11 done · M1 KİLOMETRE TAŞI TAMAMLANDI** ✅

**M1-11 done — iki denetçi ONAY** (kırmızı çizgi ihlali yok). `f416d45`: admin_users +
admin_sessions + password_resets, üçünde RLS beşlisi + REVOKE DELETE + composite same-tenant
FK. **admin'de resolver YOK** (tenant login'de bilinir — employee tap'ten farkı); admin_sessions
employee sessions'tan ayrı tablo. Q03 bcrypt (`password_hash text`, x/crypto M6-01'de). Seed
admin owner (dev-only bcrypt, round-trip doğrulandı). rls_test +3 tablo (non-vacuous: RLS DISABLE
→ RED, geri alındı). models.go make gen (deterministik). Q03 kararı: bcrypt (`8b3a0b3`).

**Denetim bulgusu (non-blocking, devredildi):** back-FK'ler (entered_by/reviewer_id/actor_id →
admin_users) M6'ya ertelendi; 00005'in "M1-11'de eklenir" yorumu artık yanıltıcı (immutable) —
ŞU AN'a M6-04/M6-01 devir maddesi + düşük notlar yazıldı.

**🏁 M1 TAMAM (11/11):** 6 migration, 11 tablo (tenants, locations, departments, employees,
sessions, tags, transactions, audit_log, transaction_reviews, admin_users, admin_sessions,
password_resets) — hepsinde RLS beşlisi; transactions/audit/reviews immutable (REVOKE+trigger);
tenant-çözümleme mekanizması (SECURITY DEFINER, GUC-anahtar denetimde reddedildi); WithTenant
(set_config, sızıntı-yok kanıtlı); ilk sqlc sorguları (make gen yeşil); RLS izolasyon+immutability
testleri (non-vacuous 3 yolla kanıtlı); KF/KM seed. Bu oturumda **M0→main merge + Q07 + Q03 +
M1-01…M1-11 + x/text CVE + redline scanner düzeltmesi** — hepsi builder→üçüncü göz (kırmızı
çizgi görevlerinde + tappa-security-auditor) döngüsünden geçti.

**Sırada:** M2-01 (ADR 0003) — **Q05 + Q06 kullanıcıya sorulacak.**

### 2026-07-25 (3. oturum, devam) — **M1-10 done** (seed) · M1 tek göreve indi

**M1-10 done — üçüncü göz ONAY.** `516be65`: `test/fixtures/seed.sql` + `ids.go`. KF 9
lokasyon + KM 5 departman, 36 çalışan, 12 tag. Bağımsız doğrulandı: idempotent (2. `make seed`
→ INSERT 0 0), 12/12 sahte-etiketli anahtar (`FAKE-WRAPPED-KEY-DO-NOT-USE-<uid>`, §4.7), yalnız
doküman IP'leri (cidr[]), Malta GPS en yakın çift 783.6 m, çift-uçlu vardiya + Rusty Bar overnight,
cross-tenant paylaşım 0, ids.go 53 UUID+12 tag DB ile birebir, yalnız master veri (transactions/
audit/reviews/sessions/admin_users hepsi 0). now()-göreli + DST-farkında Malta→UTC. Senaryo
fixtures (lost/retired plaket, invited/deactivated/null-dept/null-email çalışan).

**Sırada:** M1-11 (admin) — **M1'in son görevi.** Q03 kullanıcıya sorulacak (migration
KDF-agnostik; asıl KDF+dependency kararı M6-01). M1-11 seed'e admin owner ekleyecek + M1-09 RLS
test listesine 3 tablo.

### 2026-07-25 (3. oturum, devam) — **M1-09 done** (RLS/immutability testleri) + 2 sapma çözüldü

**M1-09 done — üçüncü göz ONAY.** `internal/db/rls_test.go` (`a033c8a`): izolasyonun ve
immutability'nin kanıtı. Üçüncü göz **non-vacuous'u 3 bağımsız yolla** doğruladı (kendi
bozup kırmızıya döndürdü, geri aldı): DB'de RLS DISABLE, trigger DISABLE, kaynak mutasyonu
`b.tenantID`→`a.tenantID`. 7 vaka + 9 tablo. M0-03 kaçış yolları kapalı (ham SQL/tenant_id
yok, pozitif kontroller, çalışma-anı `current_user`+`rolsuper/rolbypassrls` assertion'ı).
`TestStoreQueryFiltersByTenant` ayrı (izolasyon kanıtı değil). `TestResolveColumns_MatchSchema`
resolve.go drift koruması. Fixture teardown: rastgele-UUID (append-only+REVOKE DELETE teardown'ı
imkânsız kılar — imkânsızlık garantidir).

**Denetimin bulduğu 2 sapma (bloklamadı) çözüldü:**
1. **x/text CVE (`GO-2026-5970`)** — M1-07'nin pgxpool'u transitif getirmişti; `make audit`
   kırmızıydı. `go get golang.org/x/text@v0.39.0` (+x/sync v0.21.0) → govulncheck temiz,
   `make audit` yeşil. Commit `1554135`. **M1-07 denetimi bunu kaçırdı** (go build/vet/staticcheck
   CVE görmez) → agent-brief dersi: go.mod değişince `make audit`/govulncheck koş.
2. **redline R3/R5 `_test.go` yanlış-pozitifi** — RLS testi transactions UPDATE/DELETE ve
   DATABASE_MIGRATE_URL'ü meşru çalıştırıyor; yapıcı string-concat ile atlatmıştı (smell).
   Scanner düzeltildi (`--glob '!**/*_test.go'` yalnız R3 + R5-migrate-url'e; migration-beşlisi
   ve SET-LOCAL dokunulmadı), test düz literal'e döndü. **Mutasyonla dar olduğu kanıtlandı**
   (non-test .go ihlali hâlâ R3/R5 FAIL; migration ihlali hâlâ yakalanıyor). Commit `<sonraki>`.

**Sırada:** M1-10 (seed) — skill `tappa-seed`.

### 2026-07-25 (3. oturum, devam) — **M1-08 done** (ilk sqlc sorguları) · `make gen` YEŞİL

**M1-08 done — iki denetçi ONAY.** `62b70a8`: `make gen`/`build`/`dev` kırmızısı bitti.
6 tenant-kapsamlı sorgu (hepsi açık tenant_id, üretilen SQL'den okundu). `AdvanceTagCounter`
atomik CTE strict-`<` (§4.4) — canlı: 5→8 gap=2, replay→0, 2-goroutine -race tam 1 kazanan.
`GetLocationByIP` cidr[] içerme. Querier arayüzü üretildi.

**Önemli mimari bulgu:** sqlc v1.28 `SELECT ... FROM <RETURNS TABLE fonksiyonu>()`'ı
**tipleyemiyor** (ölçüldü, birkaç form denendi). → iki resolve lookup (`GetTagByUID`,
`GetEmployeeBySessionHash`) `internal/db/resolve.go`'da **elle, tipli** yazıldı; yalnız
`resolve_tag_by_uid`/`resolve_session_by_token_hash` SECURITY DEFINER fonksiyonlarını çağırır
(çıplak tablo yok), bağlamsız ham havuzda (M1-07 pool.go'nun öngördüğü dar resolver erişimi).
`resolve.sql` `-- name:`'siz kanonik-SQL belgesi olarak kaldı. ADR 0002 madde 7'ye uygulama
notu + agent-brief'e ders eklendi. Denetçiler ampirik doğruladı (bağlamsız çıplak SELECT→0,
resolver→satır — genel bypass yok).

**Q25(c):** cidr[] override **gerekmedi** (pgx/v5 varsayılan `[]netip.Prefix`, ölçüldü);
sqlc.yaml değişmedi. **WithTenant** `pgx.Tx` kaldı (RLS/resolve ham SQL ister; §7 sınırı).

**M1-09'a devredilen (bloklamayan):** store_test.go DELETE-revoked yüzünden rastgele-UUID
fixture bırakıyor → M1-09 owner-teardown ekleyebilir · resolve.go const SQL'i migration
fonksiyon imzalarıyla elle-senkron → M1-09'a sütun-sırası/tip kontrolü.

**Sırada:** M1-09 (RLS izolasyonu + değişmezlik testleri) — M0-03 3 kaçış yolu brief'e zorunlu.

### 2026-07-25 (3. oturum, devam) — **M1-07 done** (Go: havuz + WithTenant)

**M1-07 done — üçüncü göz 1. turda ONAY** (ilk Go kodu görevi). `internal/db/{pool,tenant,
tenant_test}.go` (`f73972a`): `pgxpool` sarmalayıcı (tappa_app, handler'lara açılmaz) +
`WithTenant(ctx, tenantID, fn)` — `set_config('app.tenant_id',$1,true)` param-bağlı (çıplak
SET/string concat yok), commit/rollback/panik-repanik, rollback `context.Background()` ile,
`uuid.Nil` reddi. Q27 telafisi: bağlamsız sorgu yapısal olarak imkânsız.

**Üçüncü göz üç negatif kontrolle kanıtladı** (repo-dışı kopyada): `set_config` true→false →
sızıntı testi FAIL; error-branch Commit → rollback testi FAIL; `panic(p)` silme → panik testi
FAIL. Testler vacuous değil. 5/5 -race test (aynı-backend `pg_backend_pid` sızıntı-yok kanıtı).

**İmza sapması (dokümante):** callback `func(ctx, pgx.Tx) error` — `store` M1-08'de üretilecek,
import derlenmezdi. M1-07 kartına düzeltme bloğu. Resolve erişimi M1-08'e ertelendi (havuz
açılmadı — madde 7 telafisi korundu). `go mod tidy` pgx/uuid'yi direct yaptı, **templ'i düşürdü**
(hiçbir .go import etmiyor; M2'de döner; make gen pinli @version kullandığı için etkilenmez).

**Sırada:** M1-08 (ilk sqlc sorguları) — planlı sqlc kırmızısını yeşile çevirir; resolve.sql
çözümleme sorguları + Q25(c) cidr[] override burada.

### 2026-07-25 (3. oturum, devam) — **M1-06 done** · **M1 şema katmanı TAMAM**

**M1-06 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
ihlali yok). `00005_create_transactions_audit_reviews.sql` (`d91c609`): transactions +
audit_log + transaction_reviews, üçü append-only + RLS beşlisi.

**§4.3 immutability kuşak+kemer:** açık `REVOKE UPDATE, DELETE` (default-privilege tuzağı)
+ `BEFORE UPDATE OR DELETE` trigger — satır varken **superuser tappa_owner bile** durduruldu.
Bilinen sınır (kabul): superuser DISABLE TRIGGER / session_replication_role=replica ile
atlayabilir — bilinçli defense-in-depth, mutlak değil.

**§4.6:** nullable employee/location/department/tag_uid/ctr + CHECK'ler; çalınmış-plaket
reject, flag, manuel kayıt yazılabiliyor; **`UNIQUE(tag_uid,ctr)` yok** (reddedilen replay
kaydedilebilir). transaction_reviews 3 kısıt (UNIQUE + flag-only trigger + no-self-review).
**Çapraz-tenant review YAPISAL kapalı** — composite FK ile (denetçi trigger'ı DISABLE edip
kanıtladı: FK reddediyor, trigger değil). FLAGGED onay transactions'a dokunmuyor (Q20).

**M1 şema katmanı bitti:** 8 tablo (tenants, locations, departments, employees, sessions,
tags, transactions, audit_log, transaction_reviews) + RLS her tabloda + immutability +
çözümleme mekanizması. Kalan M1: M1-07 (Go WithTenant), M1-08 (sqlc), M1-09 (RLS testleri),
M1-10 (seed), M1-11 (admin).

**Sırada:** M1-07 — ilk Go kodu görevi. Sıralama nüansı (store.Querier henüz yok) ŞU AN'da.

### 2026-07-25 (3. oturum, devam) — **M1-05 done** (tags)

**M1-05 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
görevi). `00004_create_tags.sql` (`a1bcdc4`): tags + RLS beşlisi (standart NULLIF) +
`resolve_tag_by_uid` çözümleme fonksiyonu (M1-04 kalıbı — owner tappa_resolver superuser
değil, search_path sabit, PUBLIC REVOKE, kolon-SELECT, ≤1 satır uid PK). uid `char(14)`
hex CHECK, `aes_key_ref bytea` sarmalı, `last_ctr` yalnız durum, **`UNIQUE(uid,ctr)` YOK**
(§4.4 — reddedilen replay de kaydedilebilmeli), DELETE açık REVOKE.

**Adversarial denetim (tags):** enumerate, **pg_temp poisoning** (sahte TEMP tags →
fonksiyon gerçek public.tags döndürdü), `public.tags_evil` yaratma (denied), SET ROLE
(denied), uid injection — hepsi başarısız. **aes_key_ref maruziyeti kabul edilebilir +
mimari zorunlu** (SUN/CMAC tenant bağlamından önce anahtarı ister; sarmalı ref KEK olmadan
atıl, uid public, EXECUTE yalnız tappa_app).

**İki gerekçeli sapma (denetçi sound buldu):** replaced_by same-tenant composite self-FK
(+ UNIQUE(uid,tenant_id); çapraz-tenant pointer'ı yapısal engeller) · replaced_by redundant
hex CHECK (zararsız). İkisi de güvenliği artırıyor.

**İleriye devredildi (M1-08/M1-10):** aes_key_ref-sarmalı doğrulaması şema düzeyinde
zorlanamaz → insert-yolu + seed ayrıca doğrulamalı (yukarı "ŞU AN").

**Sırada:** M1-06 (transactions immutable + audit_log + transaction_reviews) — en kritik
immutability görevi.

### 2026-07-25 (3. oturum, devam) — **M1-04 done** (employees, sessions, tenant çözümleme)

**M1-04 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor, kırmızı çizgi
görevi olduğu için kuşak+kemer). `00003_create_employees_sessions.sql`: employees +
sessions, RLS beşlisi (standart NULLIF), biyometri yok, `token_hash` UNIQUE (token asla),
composite same-tenant FK. Commit `2c42c67`.

**En kritik parça — tenant çözümleme mekanizması (ADR 0002 madde 7).** "Kimlik doğrulama
yolundaki tek delik". Kararı GUC-anahtar mı yoksa ADR'nin SECURITY DEFINER'ı mı diye
**kullanıcıya sordum → "önce denetle"**. Bir tasarım denetçisi GUC-anahtar saf-RLS
alternatifini canlı Postgres'te **iki tek-nokta hatasıyla kırdı** (SET LOCAL'siz resolve
GUC → toplamsal OR çapraz-tenant READ sızıntısı, NULLIF yakalamıyor; `FOR ALL USING`
WITH CHECK'i kopyalıyor → WRITE forge). Çevreleme **yapısal değil, disipline bağlıydı** →
**reddedildi**. ADR'nin kararı doğruymuş; gerekçesi ("saf RLS ifade edemez") yanlıştı →
madde 7 düzeltildi + GUC-anahtar reddedilen alternatif olarak kaydedildi.

**Kurulan yapısal mekanizma:** `tappa_resolver` rolü (db-init: NOLOGIN, BYPASSRLS,
NOSUPERUSER, default privilege YOK) + `resolve_session_by_token_hash` SECURITY DEFINER
(owner tappa_resolver **superuser değil**, `search_path=pg_catalog,pg_temp` sabit + gövde
`public.sessions` nitelenmiş, `REVOKE ALL FROM PUBLIC` + yalnız tappa_app EXECUTE, kolon-
düzeyi SELECT, ≤1 satır UNIQUE). Denetçiler **enumerate · search_path injection (gerçek
pg_temp.sessions kurdu) · PUBLIC EXECUTE · SET ROLE · param injection** saldırılarını
denedi, hepsi başarısız. tappa_app fonksiyon olmadan çapraz-tenant session okuyamıyor.

**db-init rölü + re-init:** kullanıcı "db-init'e ekle + re-init" seçti. `docker compose
down -v` reddedildi + Docker daemon internet kesintisinde düşmüştü → daemon'ı yeniden
başlattım, rolü tappa_owner ile **elle** oluşturdum (db-init'in taze konteynerde yapacağının
aynısı — volume silmeden, dev DB ⇄ CI tutarlı).

**DELETE tuzağı (ikinci denetçi + yapıcı buldu):** GRANT'tan DELETE çıkarmak yetmiyor —
`ALTER DEFAULT PRIVILEGES` her tabloya DELETE veriyor; açık `REVOKE DELETE` gerekti
(sessions/employees). **M1-06 transactions immutability için ZORUNLU** — yukarı "DELETE
tuzağı" bloğuna işlendi + agent-brief dersine eklendi.

**Sırada:** M1-05 (tags) — çözümleme mekanizmasının tags ayağı, M1-04 kalıbının aynısı.

### 2026-07-25 (3. oturum, devam) — **M1-03 done** (locations & departments)

**M1-03 done — üçüncü göz 1. turda ONAY.** `00002_create_locations_departments.sql`:
iki tablo, her ikisinde RLS beşlisi (`NULLIF` policy, USING+WITH CHECK). Çapraz-tenant
FK: `locations UNIQUE(id, tenant_id)` + `departments` bileşik FK `(location_id,
tenant_id)→locations` ON DELETE RESTRICT (+ doğrudan tenant FK). `static_ips cidr[]
NOT NULL DEFAULT '{}'` (Q07), `gps numeric(9,6)` (float değil), `shift_* time` +
`overnight bool`, `locations.shift_*` ve `departments.shift_*` nullable. Denetçi canlı
doğruladı: fail-closed/pozitif/izolasyon/WITH CHECK (iki tablo, tappa_app), çapraz-tenant
FK reddi (owner), cidr[] içerme, Down, R5 **mutasyonla** (GRANT+FORCE sildi→ayrı flag).

**Yapıcı savunmacı ekstra kısıtlar ekledi** (işaretledi, kapsam içi/aynı tablo): gps/shift
pair + gps aralık CHECK'leri, `(tenant_id, location_id)` bileşik indeks. Denetçi ikisini
sorguladı (bloklamıyor): shift_pair tek-yönlü vardiyayı reddediyor · overnight=true+null
vardiya kabul (tutarsız ama zararsız). İkisi de master veri, §4.6-güvenli → M4-05/M1-10'a
devredildi (yukarı "ŞU AN"da).

**Q25(c) ertelendi:** sqlc.yaml cidr[] override'ı M1-08'e — sqlc sorgu olmadan koşamaz,
doğrulanamaz (M0-04 dersi). pgx/v5 zaten cidr'i netip.Prefix'e eşliyor; M1-08'de
GetLocationByIP ile birlikte eklenip doğrulanır.

**Sırada:** M1-04 (employees & sessions) — ADR 0002 madde 7 çözümleme mekanizması
(sessions) brief'e girmeli.

### 2026-07-25 (3. oturum, devam) — **M1-02 done** (tenants migration)

**M1-02 done — üçüncü göz 1. turda ONAY.** `db/migrations/00001_create_tenants.sql`:
`tenants` tablosu + RLS beşlisi (`tenants` istisnası: scope anahtarı `id`, `tenant_id`
değil — ADR 0002 madde 5). Policy birebir `id = NULLIF(current_setting('app.tenant_id',
true), '')::uuid` (USING+WITH CHECK). Denetçi canlı doğruladı: fail-closed (bağlamsız→0,
doğru bağlam→1) + WITH CHECK (yanlış-id INSERT hatası) + pozitif kontrol, hepsi
**tappa_app** (rolsuper=f, rolbypassrls=f) ile; Down gerçekten çalışıyor (DROP TABLE
policy+grant'ı düşürüyor); redline R5 **mutasyonla** kanıtlandı (4 sabotaj yolu da
yakalandı). Yapıcı `tappa-db-migrator` idi, kapsam **yalnız migration** (sqlc/test
M1-08/M1-09'a bırakıldı — sınır korundu).

**Kararlar:** `vat_number NOT NULL UNIQUE` (global tekil, format app'te), `plan
CHECK(founding|standard) DEFAULT founding` (M6-12 founding uyarısını okuyacak),
`structure/business_type CHECK` (enum değil — goose Down temiz), `timezone` Q01.

**İki keşif kapatıldı:** (1) Makefile `migrate-new` `-s` geçmiyordu → timestamp isim
üretiyordu; `-s` eklendi, artık `00001/00002...` sequential. (2) `make gen`/`dev`/`build`
sqlc'de "no queries" ile patlıyor — **planlı** (M1-08 ilk sorguyla yeşile döner);
`make check` sqlc çalıştırmadığı için etkilenmiyor, CI yeşil.

**settings.json:** oturumda beş izin (docker compose down · git push · git commit ·
gh pr create · go get) ask→allow taşınmış; kullanıcı **olduğu gibi bırak** dedi,
şeffaf chore commit'iyle kaydedildi (§10 davranışı değişmez: istemedikçe push/PR yok).

**Sırada:** M1-03 (locations & departments) — **Q07 kararı gerekir** (static_ips tipi),
başlamadan sorulacak.

### 2026-07-25 (3. oturum) — M0 `main`'e birleşti, **M1-01 done**

**İki kullanıcı kararı.** (1) `m0-bootstrap` → `main` fast-forward birleştirildi
(`562f021`), dal silindi. (2) **Bundan sonra doğrudan `main`'de çalışılır, görev
başına dal açılmaz** (kullanıcı: "yeni proje, sürekli branch gereksiz"). CLAUDE.md
§10 buna göre güncellendi (`88b775e`); push/PR yine istemedikçe yok.

**M1-01 done — üçüncü göz 2. turda ONAY.** ADR 0002 (tenant bağlamı + RLS)
yazıldı: rol ayrımı, tx-başına `set_config('app.tenant_id',$1,true)`, normatif
politika ifadesi `NULLIF(current_setting('app.tenant_id', true), '')::uuid`
(Q27), kuşak+kemer açık filtre, `tenants` öz-koruması, MVP'de süper-admin yok, ve
**tenant çözümleme istisnası**. M0-03 ölçümleri (tappa_owner superuser + FORCE,
izolasyon testi tappa_app/DATABASE_URL, ham sorgu vs §4.5 filtresi) normatif not.

**1. tur RED — gerçek kusur.** Madde 7, çözümleme mekanizması olarak superuser
`tappa_owner`'a ait `SECURITY DEFINER` fonksiyon öneriyordu — ama superuser gövdesi
RLS'i tümüyle atlar, yani ADR'nin kendi "genel bypass açılmaz" şartını ihlal eden
**genel bir bypass**. Ben (orkestratör) briefte bu gerilimi denetçiye sordurdum;
denetçi bağımsız buldu. **Öğrenilen teknik gerçek:** saf RLS sorgunun **şekline**
göre kısıtlama ifade edemez (satır bazlı boolean, `WHERE`'i göremez) → çözümleme
kaçınılmaz olarak sınırlı bir bypass ister; iş onu **çevrelemektir** (arayüz beş
kısıtı; definer superuser olamaz; §6 FORCE altında **yalnız BYPASSRLS**).

**2. tur ONAY + iki bloklamayan gözlem kapatıldı.** (a) ADR'ye "§6 FORCE altında
salt-SELECT yetersiz, bypass yalnız BYPASSRLS olabilir" sınır netliği eklendi
(M1-04/05 tuzağını kapatır). (b) Kart madde 7'nin ADR'nin çürüttüğü "sütun bazında
kısıtlı politika" örneği düzeltildi + görünür kart düzeltme bloğu ("yanlışlanan
kartı da düzelt" dersi). Küçük doküman düzeltmeleri orkestratörce doğrulandı.

**M1-04/M1-05'e devredilen gereksinim** yukarıda "ŞU AN"da yazılı (çevrelenmiş
bypass yüzeyi, BYPASSRLS sınırı) — brief'e girmesi zorunlu.

**Sırada:** M1-02 (Migration 0001: tenants) — bekleyen karar yok.
**⏳ Kullanıcıya:** arm64 Go kurulumu hâlâ açık (iki komut, sudo), bloklamıyor.

### 2026-07-24 (2. oturum, devam) — M0-06 kapandı, **M0 TAMAMLANDI**

**M0-06 done — üçüncü göz 1. turda ONAY** (bu oturumun ilk tek-turluk onayı).
`.github/workflows/ci.yml`: `push`+`pull_request`, tek job, `actions/checkout@v4`
+ `actions/setup-go@v5` (Go **1.26.5** pinli), ripgrep kurulur, `make tools` →
`make up` → `make check` → `make audit`. **Node yok**, üçüncü parti action yok,
action'lar pinli.

**İki kart sapması ölçümle doğrulandı:** (1) `CGO_ENABLED` kartta `0` yazıyordu →
**`1`** olmalı: `make check` `go test -race` koşuyor ve linux/amd64'te race detector
cgo ister (`GOOS=linux CGO_ENABLED=0 go test -race` → `-race requires cgo`, **sıfır
test dosyasıyla bile**). (2) Postgres `services: postgres:17` bloğuyla **değil**,
`make up` (compose) ile: `services:` konteynerleri checkout'tan **önce** başlar,
repo'nun `db-init/01-roles.sql`'ini uygulayamaz → `tappa_app` rolü hiç oluşmaz.

**Q04 metni düzeltildi:** "CI'da `services: postgres:17`" cümlesi infeasible'dı ve
sevk edilen CI ile çelişiyordu; uzlaştırma notu eklendi (kararın özü değişmedi,
yalnız CI'da nasıl ayağa kalktığı). Denetçinin "yanlışlanan kartı da düzelt" bulgusu.

**M0'ın yedi görevi:** M0-01 (2 tur) · M0-02 (3 tur) · M0-03 (3 tur) · M0-04 (2 tur) ·
M0-05 (ilk commit) · M0-06 (1 tur) · M0-07 (2 tur). Biri (M6-10) proje genelinde
`skipped`. **M0 milestone tamam.**

**Sırada:** `m0-bootstrap` → `main` birleştirme (**kullanıcı kararı**, sor) → M1-01.

**⏳ Kullanıcıya hatırlatma:** arm64 Go kurulumu hâlâ açık (iki komut, sudo).

### 2026-07-24 (2. oturum, devam) — M0-07 kapandı, `redline-check.sh` yeniden yazıldı

**M0-07 done — üçüncü göz 2. turda ONAY.** Dört iş: (1) `middleware.RealIP`
router'dan çıkarıldı (SA1019; §5'te 50 güven puanı taşıyan IP'nin altına
sahtelenebilir değer koymamak) · (2) `make seed` yerel `psql` yerine
`docker compose exec` (yeni `scripts/seed.sh`) · (3) `govulncheck` **v1.6.0**'a
pinlendi · (4) `redline-check.sh` R5 dosya düzeyinden **tablo düzeyine** taşındı.
`make check` ve `make audit` **yeşil**; Bulgu 2 (stdlib CVE) Go 1.26.5 ile düşmüştü.

**1. tur RED — tarayıcının kendisi yalancıydı.** R5'te üç sessiz atlatma vardı:
kapsam-sütunu kontrolü `tenants` dışında **hiç tetiklenemiyordu** (aranan
`tenant_id`, politikanın zorunlu yazdığı `app.tenant_id` GUC adının içinde geçiyor)
· `/* */` blok yorumu beş kontrolü de susturuyordu · `-- +goose Down` bölümü Up'ın
şartlarını karşılıyordu. Yapıcının 13 vakalık sondası bunları kaçırdı çünkü
gerçekte yazılacak biçimi hiç denemedi — `agent-brief.md`'ye yeni ders olarak
işlendi ("sonda ürünün gerçek girdisiyle yapılır").

**2. tur ONAY.** `sed`+`tr` atıldı, yerine durum makineli **SQL lexer** (`sql_lex`)
+ goose Up kesici yazıldı. Denetçi lexer'a 11 kaçış yoluyla saldırdı (iç içe yorum,
E-string, dolar-etiketli gövde, `DO $$`, sonlandırılmamış tırnak…) ve **yapısal
değişmezi** doğruladı: maskeleme metni silmiyor → Up'taki her `CREATE TABLE` en
kötü ihtimalle görünür WARN üretir, asla sessiz-yeşil geçemez.

**İki konvansiyon sıkılaştı:** `tenants` istisnası artık niteliksiz/`public.` +
PK'nın `id` üzerinde olmasını arıyor (`archive.tenants` kaçışı kapandı); muafiyet
yorumu yalnız Up `^--` satırından okunuyor ve **her koşuda WARN** basılıyor
(sessiz muafiyet kapandı).

**M1'e devredilen redline notları (bloklamayan):** `E'\''` E-string lexer durumunu
bozup sonraki ifadeyi WARN'a düşürüyor (sessiz değil) — M1 migration'larında
E-string kullanılmamalı, "R5 denetleyemedi" WARN'ı elle doğrulanmalı · iç içe blok
yorumu desteklenmiyor (yalnız yanlış-pozitif yönü) · muafiyet `$$` gövdesi içinde
de okunabiliyor ama WARN'lanıyor · tek dosyada O(tablo²) performans (goose'un
küçük-migration konvansiyonuyla sorun değil).

**Kapsam dışı gözlem:** `tappa_owner` `rolsuper=t` (M0 init'ten geliyor); M0-03'te
de görülmüştü, M1-01 ADR 0002 yazılırken gözden geçirilmeli.

**Sırada:** M0-06 (CI) → M0 kapanır → `main`'e birleştir → M1.

**⏳ Kullanıcıya:** arm64 Go kurulumu iki komut, sudo parolası ister — orkestratör
tarball'ı indirip checksum'ını doğruladı, kalanı kullanıcının.

### 2026-07-24 (2. oturum) — M0-03 kapandı, altı karar alındı, blokeler bitti

**Ortam:** Docker açıldı, Go **1.26.5**'e yükseldi → `govulncheck` **temiz**.
M0-07'nin Bulgu 2'si (dört stdlib CVE) kendiliğinden düştü. Toolchain hâlâ
Rosetta (`darwin/amd64`); arm64 geçişi M0-07'ye alındı.

**M0-03 done — üçüncü göz üç tur sürdü, üçünde de gerçek kusur çıktı.**
Kabul kriterleri **ilk turda** karşılanmıştı (`tappa_app` NOBYPASSRLS/NOSUPERUSER,
iki rol ayrı ve ikisiyle de bağlanılıyor, `pgcrypto`+`citext` çalışıyor). RED'lerin
üçü de yapıcının **kart dışına çıkıp** yaptığı canlı RLS sondasının ürettiği
bulgulardan çıktı — sonda meşruydu ve değerliydi, kartın üç kriteri RLS'in *ön
şartını* ölçüyor, RLS'in kendisini değil.

1. **1. tur RED:** ölçüm doğru, çıkarım ters. "`tappa_owner` ile koşan izolasyon
   testi her zaman *sızıntı yok* der" **yanlış** — M1-09'un üç vakasında da
   gürültülü patlıyor. Ayrıca bulgunun yanlışladığı `m1-veri-katmani.md` satırına
   hiç dokunulmamıştı → repoda iki çelişik cümle.
2. **2. tur RED:** düzeltme olarak eklenen kriter yalnız **rolü** bağlıyordu.
   Oysa tehlike **sorgunun şekli**: `ctx=B, WHERE id=1 AND tenant_id=B` biçimi
   iki rolde de 0 satır verir — kritere tam uyumlu bir test RLS'i hiç sınamaz.
3. **3. tur ONAY.** Kriter iki boyutlu oldu (rol **ve** ham sorgu şekli), §4.5 ↔
   izolasyon testi ayrımı yazıldı, düşen "test edilir" garantisi geri kondu,
   filtreli biçim **ayrı** ve *izolasyon kanıtı sayılmayan* bir vaka oldu.

**M1'i bağlayan iki ölçüm:**
- `app.tenant_id` GUC'una bir kez **yazılınca** bağlantıda `NULL`'a dönmüyor, `''`
  kalıyor (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de). Tetikleyici **yazma**, kullanım
  sayısı değil. → **Q27**.
- `FORCE ROW LEVEL SECURITY` tablo **sahibini** bağlar, **superuser'ı bağlamaz**;
  `tappa_owner` initdb'nin bootstrap superuser'ı olduğu için kaçıyor. NOSUPERUSER
  bir sahiple ölçülerek doğrulandı (`ENABLE`-only → 3 satır, `+FORCE` → 0).

**Altı karar:** Q01 (`tenants.timezone` + `locations.timezone` override) ·
Q04 (DB testleri yerel Postgres) · Q26 (toolchain yükseltildi, arm64'e geçilecek) ·
Q25 a/b/d (seed `docker compose exec`, govulncheck pinlenir, redline R5 genişler) ·
**Q27** (`NULLIF` sarmalayıcısı — CLAUDE.md §6 güncellendi). Açık soru 14 → **11**.

**CLAUDE.md §6'ya iki madde eklendi:** politikaların `NULLIF`'li biçimi ve
"izolasyon testi ile üretim sorgusu farklı şekiller ister" ayrımı. İkincisi
olmadan §4.5'in kuşak+kemer kuralı, RLS testini sessizce anlamsızlaştırıyordu.

**M1-09'a devredilen üç kaçış yolu** yukarıda "ŞU AN" bölümünde yazılı — brief'e
girmeleri zorunlu.

**Sırada:** M0-07 (kapsamı büyüdü) → M0-06 → `main`'e birleştir → M1.

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

### 2026-07-24 — M0 yürütmeye başlandı: orkestrasyon + üçüncü göz

Çalışma modu değişti ([README.md](README.md) → Çalışma modu): ana oturum iş
yapmaz, her görevi bir Opus alt ajana yaptırır ve **ayrı** bir üçüncü göz ajanı
onaylayana kadar düzelttirir.

Dört görev kapandı: **M0-01** (2 tur), **M0-02** (3 tur), **M0-04** (2 tur),
**M0-05** (ilk commit, sıradan öne alındı). Commit'ler: `7e12f37`, `e6d9a63`,
`2521d48`. Dal `m0-bootstrap`.

**Dördü de ilk turda RED aldı ve her seferinde gerçek bir kusur çıktı** —
hayali bulgu yok. En değerlisi M0-04'teki üç bozuk `sqlc.yaml` override'ı
(nullable `uuid` → geçersiz Go · `inet` → var olmayan paket, üstelik sqlc
exit 0 veriyordu · nullable `timestamptz` override'ı hiç yoktu). Üçü de
iskeleden beri oradaydı ve M1'de kesin patlardı.

Kart hataları da bu turlarda çıktı ve düzeltildi: M0-01'in `go run` kriteri
ulaşılamazdı (`.env`'i Makefile yüklüyor) · M0-02'nin `go mod tidy` adımı kendi
önceki adımlarını siliyordu · M0-04'ün sqlc kriteri fazla gevşekti.

Yeni görev **M0-07** (`make check` + `make audit` yeşile alma) ve yeni soru
**Q26** (Go ≥1.26.5, arm64) denetimden doğdu.

**M0'ın kalan üçü de kullanıcı girdisi bekliyor** — burada duruldu.

Bağlam sıkıştırması öncesi [agent-brief.md](agent-brief.md) yazıldı: yapıcı ve
denetçi brief şablonları, her turda tekrarlanan sabit kurallar ve M0'da
öğrenilen dokuz ders. Bunlar o ana kadar yalnız sohbette taşınıyordu; artık
repoda.

**Kullanıcıdan beklenen dört girdi:** Docker Desktop (M0-03) · Q26 Go ≥1.26.5
arm64 (M0-07) · Q04 DB testi hedefi (M0-06) · Q01 zaman dilimi (M1-02).
