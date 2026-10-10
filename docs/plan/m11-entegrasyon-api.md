# M11 — Entegrasyon API'si (KF-RMS · KM-ERP)

> **Durum:** PLAN, **4. sürüm** (2026-10-10, 28. oturum). Kod yok.
> 1., 2. ve 3. sürüm (`a169391`, `e0ad91f`, `41c956d`) ikişer bağımsız denetimden
> (üçüncü göz + güvenlik merceği) **RED** aldı; 3. turda bloklayan bulgu kalmadı,
> imleç tasarımı gerçek Postgres'te ölçülerek doğrulandı. Bu sürüm 3. turun iki ORTA
> ve düşük bulgularını işler — eşleme §9'da. Kullanıcı kararları: K-1, K-2, K-3,
> K-19, K-25, K-26 (§3). Sıradaki adım: dar kapsamlı 4. tur denetim, ONAY gelirse API-0.
>
> **Kaynak:** kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve
> KM-ERP entegrasyonu)"*, 2026-10-09. **Repoya konmadı** — depo public ve doküman
> müşterinin iç webhook adresini taşıyor. "spec §N" o dokümanın bölümüdür.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"** diye yazılır; çıplak
> "§N" bu planın kendi bölümüdür.

**Amaç.** Tenant'ın panelinden ürettiği **API anahtarlarıyla** dış İK/ERP
sistemlerinin (ilk müşteri: KF-RMS ve KM-ERP) Tappa'ya lokasyon ve çalışan
göndermesi, punch'ları **imleçli akıştan** çekmesi ve **webhook** ile anında
alması. ZKBio'nun yerini alan entegrasyon yüzeyi budur.

**Neden ayrı milestone.** Bugün dış dünyaya açık hiçbir JSON API, Bearer kimliği,
webhook, OpenAPI ya da Idempotency-Key yok (ölçüldü). Bu iş yeni bir **güvenlik
sınırı** (makineden makineye kimlik), yeni bir **giden ağ yüzeyi** (müşterinin
seçtiği URL'ye istek) ve yeni bir **sıralama sözleşmesi** (punch akışı) açar —
üçü de ADR ister (CLAUDE.md §10).

---

## 1. Spec ↔ Tappa — bugünkü durum (ölçüldü, iki turda yeniden üretildi)

| Spec beklentisi | Tappa bugün | Plan |
|---|---|---|
| İki şirket (KF/KM), **her çalışan iki şirketin her lokasyonunda okutabilir** | KF ve KM **iki ayrı tenant**; gruplama yok. Yabancı tenant plaketi → `sys:tenant-mismatch` → **403, kayıt yok** (`internal/policy/guardrails.go:306-319`, `internal/handler/checkin.go:227-233`) | **K-1** |
| Location: `externalRef`, `timezone`, `active` | external id yok · saat dilimi **tenant** sütunu · aktif bayrağı yok, silme sert `DELETE` (`db/queries/locations.sql:369`) | API-1, K-7, K-20 |
| Tag panelden lokasyona bağlanır | Var (`tags.uid` küresel PK). QR satırları da `tag_uid` taşır (`internal/domain/checkin/checkin.go:1282-1285`) | `tagId = tag_<uid>` |
| Employee: `externalRef`, `employeeCode`, `firstName`, `lastName`, `email`, `active`, `activated` | external id/kod **yok** · tek `full_name` · `email citext`, tekilliği `employees_tenant_email_key (tenant_id, email) WHERE email IS NOT NULL` — **pasif satırlar dahil**; kod çakışmayı **kısıt adıyla** tanıyor (`internal/domain/tenant/staff.go:706-717`, `db/queries/employees.sql:783-784`) · `status` + `activated_at` var · `location_id NOT NULL` · `updated_at` yok | API-1, K-14, K-21, K-24 |
| Yeni çalışana **otomatik** aktivasyon e-postası | Çalışan eklemek davet **göndermez**; davet e-postası **senkron**. Tavanlar: tenant 50/saat 300/gün · çalışan 3/saat · kutu 5/saat 20/gün (`(tenant, kutu)` anahtarlı) · süreç kesicisi 300/saat. **EM-5B (`c447c88`) `main`'de** (`origin/main` = `77cfb98`) | API-6, K-16, K-17 |
| **İlk dokunuş = ilk punch** | ADR 0026: aktivasyon dokunuşu **`transactions` satırı yazmaz** | **K-2** |
| "Doğrulanamayan dokunuş punch'a dönüşmez" | `flag` kaydı yazılır, onay kuyruğuna düşer (CLAUDE.md §4.6); onay `transaction_reviews` (append-only, `UNIQUE(transaction_id)`) | K-4 |
| Düzeltme/iptal → yeni `version` | Geçerli kaydı iptal yolu yok; düzeltme = bağsız yeni manuel satır (ADR 0011) | K-5 |
| Manuel punch'lar RMS/ERP'de (spec §9) | Tappa'da `channel='manual'` kayıt var | K-6 |
| İmleçli akış, **kayıt sırasına göre**, geç gelen kaçmaz | `transactions`'ta monoton anahtar yok (`gen_random_uuid()`); `created_at` ≠ commit sırası | API-2, K-13 |
| Webhook + imza + tekrar deneme | DB outbox yok; dış HTTP yalnız VIES; dial-time SSRF koruması yok, pod egress'i açık | API-2, API-9, API-10, K-19 |
| Sistem başına API anahtarı | Yok. Token kalıbı var (32 bayt, HMAC'li hash, redakte tip) | API-3 |
| Idempotency-Key, `limit ≤ 1000`, JSON hata gövdesi | Yok. `httpx.Limiter` (bellekte, `replicas: 1`) ve 429'da `Retry-After` **var** (`internal/httpx/ratelimit.go:551`) | API-4 |
| OpenAPI + sandbox | Yok. Dokunuş simülatörü dört katlı kapının arkasında: `TAPPA_ENV=dev` + loopback `BaseURL` (`internal/config/config.go:731-745`, `internal/handler/devtap.go:71-78`). Sertleştirmeler `IsProd()` = `Env == "prod"` dalında (`config.go:434`) | API-11, API-12, **K-3** |
| Bir telefon tek bir çalışana bağlı | Var — tarayıcı başına tek `tappa_session` çerezi | — |

**Eşleme notu — KM'nin "departmanları".** Spec'te KM-ERP departmanları
(`DEP-1`…) **lokasyon** olarak gönderir ve etiket onlara bağlanır. Tappa'da
plaket **lokasyona** bağlanır. KM'nin her departmanı Tappa'da **bir lokasyon**
olur; Tappa `departments` v1 API'sinde görünmez.

---

## 2. Sözleşme farkları — entegratöre bildirilecek

API-12'nin teslim ettiği rehberde bu liste **açıkça** yazılır.

1. Aktivasyon dokunuşu punch **değildir**; ilk punch ikinci dokunuştur (K-2).
2. `flag` verdict'li dokunuş yalnız **müdür onaylarsa**, **onay anında** akışa girer; reddedilen hiç gelmez (K-4).
3. v1'de `version` hep `1`, `voided` üretilmez (K-5). Alanlar sözleşmede durur.
4. Ek alan: `channel` (`nfc` | `qr`). Her iki kanalda da `tagId` doludur.
5. `punchTime` = Tappa'nın `occurred_at`'ı: telefonun **beyan ettiği** an. Çevrimdışı
   kuyruk **varsayılan** 120 sn'ye kadar `ok` kalır (tenant'ça ayarlanabilir baseline —
   `internal/policy/baseline.go:109-118`); daha eskisi onaya düşer; tavan 72 sa.
   `recordedAt` = sunucunun kaydı yazdığı an.
6. 60 sn içindeki tekrar dokunuş (`ignored`) punch **değildir**.
7. Lokasyon `timezone`'u tenant'ın saat dilimine eşit olmalı (bugün `Europe/Malta`), değilse 422 (K-7).
8. Lokasyonda `active:false` **yalnız etikettir**: dokunuş kararını değiştirmez (K-20).
9. Yeniden işe alım (pasif → `active:true`) **yeni `employee.id`** üretir; `externalRef` aynı kalır (ADR 0010).
10. API ile açılan lokasyon panelden **GPS/IP** tanımlanana kadar dokunuşları onaya düşer → onaylananlar sonradan gelir.
11. Çalışan e-postası ASCII olmalı ve aynı tenant'ın bir yönetici adresi olamaz (422);
    tenant içinde aktif başka bir çalışanda kullanılıyorsa 409 `email_unavailable` (K-24).
    Davet her zaman **kuyruğa** girer (`activationEmail: queued`); tavanlar gönderimi
    geciktirir, isteği reddettirmez (K-17).
12. `resend-activation`: kuyrukta bekleyen ya da son bir saat içinde gönderilmiş canlı
    bir davet varken **etkisizdir** (202 `already_queued` / `recently_sent`) (K-17).
13. Aktive olmuş çalışana `resend-activation` → 409 `already_activated` (cihaz değişimi yalnız panelden).
14. Aktive olmuş çalışanın e-postası değişirse **davet gitmez** (spec §4.2'den sapma — K-15).
15. Aktivasyon yalnız NFC ile (QR aktive edemez; iPhone X ve öncesi aktive olamaz).
16. Webhook'ta sıra garantisi yok; tekilleştirme `id` + `version` ile.
17. Manuel kayıtlar akışa girmez (K-6).
18. Geçmiş sorgusu (`from`/`to`) en çok 31 gün, sayfalı.
19. `updatedSince` (rehber) **10 dakikalık örtüşme penceresiyle** çalışır; tekrar
    dönen kayıtlar `id` ile ezilerek yazılır (K-21).
20. **Sunucu yeniden başladığında (her deploy dahil)** bekleyen webhook tekrar
    denemeleri ve kuyruktaki davetler, o tenant'ın **bir sonraki dokunuşuna ya da API
    çağrısına** kadar bekler (K-19). İmleçli akış bundan etkilenmez — güvenlik ağı odur.
21. Panelden eklenmiş ve henüz `externalRef` ile bağlanmamış bir çalışanın punch'ı
    akışta `employee.externalRef: null` ile gelir (K-18 runbook'u canlı öncesi hepsini bağlar).
22. Pasifleştirme (`DELETE` ya da `PUT active:false`) anahtar başına **saatlik bir
    bütçeyle** sınırlıdır; aşılınca 429 + `Retry-After` (K-25).
23. API ve webhook, operatörün tenant için açtığı erişimle çalışır (K-22).
24. Ev lokasyonu olmayan (API'den yönetilen) çalışanın punch'ına Tappa'nın "farklı
    şubede dokundu" notu **düşmez** (CLAUDE.md §5; `internal/domain/tap/decide.go:133-134`
    ev lokasyonuyla karşılaştırır) — bu bilgi zaten `location` alanında (K-14).

---

## 3. Kararlar

### Kullanıcı kararları

| # | Karar | Seçilen | Bedeli (kayıtlı) |
|---|---|---|---|
| **K-1** | Çapraz şirket dokunuşu (CLAUDE.md §4.5'e değer) | **(A) Tek tenant, iki şirket**; ek bedellerle **yeniden onaylandı** (2026-10-09) | (1) KM yöneticileri kendi hesabını ancak **EM-12** ile alır → **EM-12 M11'in canlı ön koşulu** · (2) tenant markası KM çalışanının ekranında KF'ninki · (3) eski KM tenant'ı operatör kapatana kadar faturalanır · (4) bir şirketin yöneticisi diğerinin kayıtlarını (e-postalar dahil) görür/onaylar · (5) politika ve saat dilimi paylaşılır |
| **K-2** | Aktivasyon dokunuşu punch mı? | **ADR 0026 kalır** (2026-10-09) | §2 madde 1 |
| **K-3** | Sandbox | **Ayrı alan adı + ayrı derleme** (2026-10-09; 1. sürümdeki `sandbox.taptime.mt` + `TAPPA_DEV_TOOLS` kurulamaz çıktı) | Alan adı + DNS; ayrı imaj (§4.8) |
| **K-19** | Arka plan işçisi tenant'lar arası tarama yapsın mı? | **Etkinlikle kurulan işçi — tarama YOK**, CLAUDE.md §4.5 değişmez (2026-10-09) | §2 madde 20 |
| **K-25** | API'den pasifleştirme ADR 0010'un iki adımlı onayını atlar | **Anında + anahtar başına saatlik bütçe** (2026-10-10) | Bütçe aşımı 429 + owner'a panel uyarısı + audit; ADR 0010'a sapma notu (§4.9) |
| **K-26** | Askıda savunma yazmaları | **Açık kalır**: anahtar iptali ve uç nokta devre dışı bırakma (2026-10-10) | ADR 0025'e iki satırlık sapma notu; askının geri kalanı ADR 0025'e birebir (§4.7) |

**Prod durumu (kullanıcı):** KM tenant'ı prod'da var, dokunuş verisi yok. KM
şirketi KF tenant'ında açılır; KM lokasyon ve çalışanları **KM-ERP'nin kendi API
çağrılarıyla** gelir; eski KM tenant'ı operatör yüzeyinden kapatılır.

**⚠️ KM plaketleri — API-13 öncesi kullanıcıya sorulacak (CLAUDE.md §4.5'e değer).**
`tags.uid` **küresel** PK, satır silinmez (silinirse çip kalıcı kilitlenir),
`tappa_app`'in `tags.tenant_id` üzerinde UPDATE yetkisi yok (ölçüldü). Yeniden
encode UID'yi değiştirmez → "yeniden encode" bir seçenek **değil**. Gerçek seçenekler:
(i) **satırı taşımak** — `tappa_owner` ile koşan, audit'li, **tek seferlik runbook
betiği**; `location_id` yeniden eşlenir (bileşik FK), `replaced_by` zinciri birlikte
taşınır; tags'e FK veren yalnız iki kısıt var — `tags_replaced_by_fk` ve
`transactions_tag_fk` (ölçüldü); ön koşul: KM tag'lerine bağlı **sıfır** `transactions`
satırı (reject dahil; aksi hâlde tek "çözüm" CLAUDE.md §4.3 tetikleyicisini kapatmak
olurdu — **yasak**). (ii) **yeni fiziksel plaketler** — eski satırlar arşiv tenant'ta
emekli kalır. Anahtar sarmalının AAD'si yalnız UID (`internal/sun/keys.go:22`) → (i)
sarmalı bozmaz. **Kesim sırası:** plaketler taşınmadan (ya da yenileri takılmadan)
hiçbir KM çalışanı birleşik tenant'ta aktive edilmez.

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe |
|---|---|---|
| K-4 | Akışa giren punch = `verdict='ok'` ∧ kanal ∈ {`nfc`,`qr`} ∧ ¬`practice`, **veya** aynı kanal/practice koşulunu sağlayan **onaylanmış** `flag`. `reject`/`ignored`/reddedilmiş flag/manuel/practice **asla** | Spec §3.5; CLAUDE.md §4.6 Tappa içinde karşılanır. Dev DB'de `practice=true` ∧ `flag` satırı var (4693, ölçüldü) → onay tetikleyicisi de tam koşulu uygular |
| K-5 | v1'de iptal yolu yok → `version=1`, `status='valid'` | ADR 0011, 0009 |
| K-6 | Manuel ve tarihsel practice satırlar akışa girmez; entegre şirkette manuel kayıt formunda uyarı | Spec §9; çift sayımı önler |
| K-7 | Lokasyon başına saat dilimi sütunu **yok**; farklıysa 422 | Raporlar tenant saat dilimiyle (CLAUDE.md §6) |
| K-8 | Dış kimlikler: `emp_`/`loc_`/`pch_`/`dlv_` + UUID; `tag_` + uid | Önek tip karışıklığını yakalar |
| K-9 | Ayrı host `api.taptime.mt` (`TAPPA_API_HOST`), iki yönlü host kapısı (mevcut `operatorHostOnly` tek host bildiği için genelleştirilir) | Panel çerezleri API host'una gitmez; HTML rotaları orada 404 |
| K-10 | Anahtar: `tt_live_` öneki + 43 karakter base64url (sandbox'ta `tt_test_`); depoda kendi env anahtarıyla HMAC-SHA256. Prod yalnız `tt_live_`, sandbox yalnız `tt_test_`; yanlış önek DB'ye gitmeden 401 | Mevcut token kalıbı; önek sızıntı taramasını mümkün kılar |
| K-11 | İmza spec §4.5 biçiminde (`sha256=` öneki + gövdenin hex HMAC-SHA256'sı); ek zaman damgası başlığı **yok** | İmzaya girmeyen başlık sahte güven verir |
| K-12 | Uç nokta adresi ve imza anahtarı **panelden** (owner); anahtar yalnız oluşturma ve döndürmede **bir kez** gösterilir, "göster" yok | Mühürlü değer geri açılabildiği için "göster" ele geçirilmiş oturuma sahte imzalı punch yollatırdı |
| K-13 | **İmleç = tenant başına sayaç.** `feed_counters(tenant_id, last_seq)` satırı tetikleyici içinde `INSERT … ON CONFLICT (tenant_id) DO UPDATE … RETURNING` ile artırılır (ilk olay satırı da açar); satır kilidi commit'e kadar tutulur → `seq` **tenant içinde yoğun ve commit sırasıyla** artar. Küresel dizi ve advisory kilit **yok** (2. sürümdeki tasarım atıldı) | Ölçüldü (2. tur): küresel dizi `CACHE 1` değilse kilit altında bile 1234 olayın 1016'sı kaçıyordu; küresel `seq` ayrıca tenant'lar arası hacim sızdırıyordu. Sayaç satırı ikisini birden yapısal olarak kapatır. **3. turda ölçüldü:** 3 koşuda (1255–2416 olay, %10 abort, %10 savepoint geri alımı) kaçan 0; kilitsiz dizi ve ayrı işlemde sayaç mutasyonları 843 / 408 kaçırdı; ilk-olay yarışı 1700 olayda 0. İspat §4.4 |
| K-14 | `employees.location_id` API'den yönetilen satırlarda NULL olabilir: `CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL)`; manuel kayıt formu lokasyon seçtirir (`db/queries/transactions.sql:354` bugün `e.location_id` okuyor) | Spec'te ev lokasyonu yok. **API-1 önce `location_id`'nin bütün okuyucularını sayar;** kabul edilemezse geri dönüş: opsiyonel `homeLocationRef`. **Bilinen bedel:** böyle bir çalışanda `crossLocation` hep yanlış → "farklı şube" notu düşmez (§2 madde 24) |
| K-15 | E-posta değişince: bekleyen davetler **aynı işlemde** emekliye ayrılır + audit (panel kuralı, ADR 0022 §7); çalışan aktive değilse yeni davet isteği kuyruğa girer; aktive çalışana davet gitmez | Eski kutudaki kodun çalışanı devralmasını önler |
| K-16 | API'den yeni çalışan = otomatik davet | Spec §4.2; panel davranışı değişmez |
| K-17 | **Davet kuyruğu** ayrı tablo: `invite_requests(tenant_id, employee_id, requested_at, lease_until, attempts)` — `employees`'e kira sütunu eklenmez (rehberin `updated_at`'ını kirletmesin). İşçi kodu **gönderim anında basar**, eski davetleri **aynı işlemde** emekliye ayırır ve yollar; kira süresi gönderim bütçesinden (10 sn + 5 sn audit) uzundur (60 sn) — iki pod aynı daveti iki kez basamaz. **`resend-activation` istek anında hiçbir daveti öldürmez**: canlı istek ya da son 1 saatte gönderilmiş davet varsa etkisiz. Tavanlar **şirket başına** ve **panel ile kuyruk aynı sayacı paylaşır**: `(tenant, şirket)` 50/saat 300/gün, `(tenant, şirket, kutu)` 5/saat 20/gün; çalışan 3/saat ve süreç kesicisi aynı. **İkinci ve sonraki şirketi yalnız operatör açar** (§4.1) → herkese açık kayıtla açılan bir tenant tek şirketlidir ve süreç kesicisinden bugünkü gibi en çok 50/300 alır | 2. tur: istek anında emekliye ayırma, resend döngüsünü çalışanı süresiz kodsuz bırakan bir engelleme aracına çeviriyordu; tenant-ortak tavan da KM'nin KF davetlerini geciktirmesine izin veriyordu (EM-7C dersi). 3. tur: şirket açmak sınırsız olursa şirket başına tavan, tek bir kaydın 300/saatlik süreç kesicisini — herkesin kurtarma e-postası dahil — doldurmasına izin veriyordu |
| K-18 | İlk bağlama: `PUT /employees/{ref}` bilinmeyen ref için önce **aynı şirkette, pasif olmayan, `external_ref`'i boş, e-postası eşleşen** çalışanı bağlar (`employee.linked` audit); yoksa yeni satır açar. Lokasyonlarda otomatik eşleme yok — owner paneldeki "External ref" alanını doldurur | Pilot verisi çift kayda dönüşmesin; KM şirketi boş başladığı için KM anahtarı hiçbir KF satırını bağlayamaz |
| K-20 | Lokasyon `active` alanı yalnız etiket; karar motoruna girmez | Kararı etkilemesi CLAUDE.md §5 + politika + ADR ister |
| K-21 | `employees.updated_at` **tetikleyiciyle**, yalnız rehbere görünen sütunlar (ad, kod, `external_ref`, durum, şirket) değişince `clock_timestamp()`; rehber `updated_at >= updatedSince − 10 dk` döndürür ve `asOf` verir | Bugün dört UPDATE yolu var (`invites.sql:250`, `employees.sql:531/612/793`); elle güncellemek birini unutur |
| K-22 | API ve webhook bir tenant için **yalnız operatör açınca**: `tenants.api_enabled_at`; **açan ve kapatan** tek yazar yeni bir `op_*` fonksiyonu (ADR 0021 sınıfı, `operator_audit_log`). Kapı **her yolda**: anahtar çözümü, uç nokta oluşturma, test ping, yeniden gönderim, tetikleyicinin teslim satırı yazması ve işçinin gönderim anı. Kapalı tenant'ın olayları yine yazılır (akış), teslim satırı yazılmaz | `/signup` herkese açık: aksi hâlde herkes sunucumuza istediği adrese istek attırabilirdi. 2. tur: kapı yalnız anahtar yolundaydı |
| K-23 | API kaynaklı audit: `actor_id = api_keys.id`, `detail.via = "api"` | Aktör bir admin değilken izin kaynağı yine adlandırılmalı |
| K-24 | **Şirket sınırı her sorguda:** API araması `(tenant_id, company_id, external_ref)` ile; tekillikler: çalışan `(tenant_id, company_id, external_ref) WHERE status <> 'deactivated'`, lokasyon `(tenant_id, company_id, external_ref)`. **E-posta tenant genelinde tekil kalır** ama yalnız pasif olmayanlarda: `(tenant_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'` — **kısıt adı `employees_tenant_email_key` korunur** (kod adıyla tanıyor). Çakışma şirketten bağımsız aynı 409 `email_unavailable` | 2. tur: şirket başına e-posta, KM'nin bir KF çalışanının adresiyle KM satırı açıp onu KF markalı davetle KM bordrosuna bağlatmasına izin veriyordu. Kalan "adres kullanımda" bilgisi **kabul edilmiş artık risk**: K-1 bedeli 4 gereği KM yöneticileri o adresleri panelde zaten görüyor. Tek şirketli tenant'ta yan etki: pasif satır adresi bırakır → ayrılan biri aynı adresle yeniden eklenebilir (ADR 0010'un "yeni kayıt" çaresiyle uyumlu; ADR 0010'a not) |
| K-27 | Olay satırı, yazıldığı anda **lokasyonun ve çalışanın şirket kimliğini** kopyalar; kapsam ve yönlendirme yalnız bu kopyadan. `employees.company_id` değiştirilemez (şirket geçişi = pasifleştir + yeni satır) | 2. tur: kapsam okuma anında hesaplanırsa lokasyonun şirketi değişince geçmiş punch'lar başka sisteme kayıyor ya da hiç gelmiyordu |
| K-28 | **`IsHardened()` = `prod` ∨ `sandbox`**; üretim kodundaki **dokuz** `IsProd()` çağrısının dokuzu da bu yükleme geçer: `internal/config/config.go:326` (güvenilir proxy), `config.go:1000` (SMTP host), `internal/db/pool.go:119` (ayrıcalıklı rol reddi), `internal/session/cookie.go:136`, `internal/adminauth/cookie.go:109`, `internal/handler/cookies.go:270` (Secure çerezler), `signupstate.go:440`, `logincontext.go:297`, `activate.go:184`. `EnvDev`'e bağlı üç kapı (`config.go:738`, `devtap.go:71`, `sun/mint.go:78`) sandbox'ta kapalı kalır | `TAPPA_ENV=sandbox` aksi hâlde internete açık, çok taraflı bir kurulumda bunları sessizce kapatırdı (2. tur, iki denetim) |

---

## 4. Tasarım özü (ADR 0027 ve 0028'de normatif olacak)

### 4.0 Ortak şema kuralı — `arwd` varsayılanına güvenilmez

Üretimde `pg_default_acl` = `tappa_app=arwd/tappa_owner`; yeni tablo dört fiille doğar
(`deploy/README.md:5722-5746`, ölçülmüş), taze CI veritabanında ise `ar` ile — yani CI
fazla yetkiyi **göremez**. Bu yüzden M11'in **her** yeni tablosunda (`companies`,
`api_keys`, `api_idempotency`, `invite_requests`, `punch_events`, `feed_counters`,
`webhook_endpoints`, `webhook_deliveries`): `REVOKE ALL … FROM tappa_app`, ardından
yalnız gereken fiillere ve **sütunlara** açık `GRANT` (R5'in tablo başına GRANT şartını
da bu karşılar). Tablo düzeyi UPDATE sütun kısıtını ezer (`deploy/README.md:5701`) —
bu yüzden UPDATE hep sütun listesiyle verilir. **Kabul:** CI, migration'lardan önce
prod'un `arwd` varsayılanını kurar ve her yeni tabloda dört fiili `has_table_privilege` /
`has_column_privilege` ile ölçer.

### 4.1 Şirket boyutu (K-1 = A)

- `companies(tenant_id, id, code, name, vat_number NULL, is_default, created_at)` —
  RLS beşlisi; `UNIQUE(tenant_id, code)`; `UNIQUE(tenant_id) WHERE is_default`.
  Migration her mevcut tenant'a **bir varsayılan şirket** açar; **`/signup`** yeni
  tenant'ın varsayılan şirketini tenant ve lokasyonlarla **aynı işlemde** açar
  (`internal/domain/signup` — `locations.company_id NOT NULL` olunca aksi hâlde kayıt kırılır).
  Owner yalnız kod ve adı düzenler (KF tenant'ının varsayılanı canlı öncesi `KF`).
  **İkinci ve sonraki şirketi yalnız operatör açar** — yeni bir `op_*` yazarı (ADR 0021
  sınıfı, `operator_audit_log`); KM şirketi böyle açılır. Gerekçe: şirket başına davet
  tavanı (K-17) şirket sayısıyla çarpılır; herkese açık kayıt tek şirketle kalmalı.
- `locations.company_id`, `employees.company_id` — NOT NULL, bileşik FK
  `(company_id, tenant_id)`; `employees.company_id` değiştirilemez (K-27).
- Tap motoru **değişmez**: şirket karar girdisi değildir; dokunuş tenant içidir.
- Panel: şirket kodu/adı (owner), listelerde şirket sütunu/filtresi, çalışan eklerken
  şirket seçimi. Panel yetkileri tenant geneli (K-1 bedeli 4); EM-12 şirket kapsamını ele alır.
- Tek şirketli müşteri için tek görünür fark: pasif bir çalışanın e-postası artık
  yeniden kullanılabilir (K-24).

### 4.2 API anahtarı

- `api_keys(tenant_id, id, company_id, name, token_hash, prefix_hint, created_by, created_at, last_used_at, revoked_at)` —
  RLS beşlisi; `token_hash` **küresel UNIQUE**.
- Kimlik: Bearer → biçim kontrolü (ortamın öneki + 43 base64url; değilse DB'ye gitmeden
  401) → **IP başına kova** → **yedinci resolver** `resolve_api_key_by_hash`
  (`tappa_resolver` sahipli SECURITY DEFINER; ADR 0002 §7: girdi yalnız hash, dönüş
  `token_hash` UNIQUE'iyle tek satır, sabit `search_path`, nitelikli adlar, dinamik SQL yok).
  Mevcut altı resolver'ın **"taşı, uygulama"** ilkesine uyar (`internal/db/resolve.go:100-102`):
  dönüş `(tenant_id, key_id, company_id, revoked_at, api_enabled_at)`; uygulama aynı
  istekte karar verir — iptal → **401**, tenant'ın API erişimi kapalı → **403**.
  **Önbellek yok.** `tappa_resolver`'a yeni yetki: yalnız `SELECT (id, api_enabled_at) ON tenants`
  (birincil anahtar join'i — ADR 0002 §7(ii) bozulmaz; ADR 0002'ye not).
- Sonraki her sorgu `WithTenant` + açık `tenant_id` + **`company_id`** filtresi (K-24).
- Yetki (spec §4 tablosu): yazma = anahtarın şirketinin çalışan/lokasyonu; okuma =
  `scope=location` (olayın **kopyalanmış** lokasyon şirketi = anahtarın şirketi) +
  `scope=employer` (olayın kopyalanmış çalışan şirketi = anahtarın şirketi) + rehber (tenant geneli, e-postasız).
- Panel (owner): oluştur (bir kez göster) · listele (ad, önek ipucu, son kullanım) · iptal.
  Audit: `api_key.created`, `api_key.revoked`. `last_used_at` dakikada en çok bir kez.
- **Oran sınırı iki katlı:** IP başına kova resolver'dan **önce**, geçerli anahtarda anahtar başına kova.
- **Log ve tarama:** anahtar, hash'i, `Authorization` başlığı, uç nokta imza anahtarı,
  webhook URL'si ve `*url.Error` metni asla loglanmaz. Tarayıcı eklemeleri **önce bugünkü
  ağaçta sayılır**: yalın `kek`/`signature`/`authorization` kökleri bugün 35 satırı kırmızıya
  çevirir (3. tur ölçtü — `cmd/rotatekek`, `internal/sun/keys.go` vb.) ve R7'nin "kelimeyi değil
  değeri hedefle" ilkesine aykırıdır. Bu yüzden: R7'ye yalnız ağacı kırmızıya çevirmeyen
  kökler (`api_?key`, `bearer`, `signing_?key` — sayılarak), R7b'ye yeni ad alanları,
  R7d'ye **değer biçimli** kalıplar (API anahtarı öneki ve imza anahtarı öneki — §4.5);
  kalan durumlar adıyla muafiyet. Her ekleme **kasıtlı ihlalde kırmızı** testiyle (API-3).
  CLAUDE.md §7 yasak listesine API anahtarı ve uç nokta imza anahtarı eklenir (API-0).
- **Env anahtarları** (`TAPPA_API_KEY_HMAC_KEY`, `TAPPA_WEBHOOK_KEK`) `namedKeys()`'e girer
  ve **bütün** anahtarlarla çift çift karşılaştırılır (bugünkü ayrım kontrolü yalnız
  operatör çiftlerine bakıyor — `config.go:688-692`; genelleştirilir). `config.go:688-692`'nin
  kendi uyarısı gereği genelleştirme ölçülmemiş bir üretim yapılandırmasının açılışını
  reddettirebilir → API-13 runbook'u deploy'dan önce prod'daki anahtar çiftlerinin eşitliğini
  **değerleri basmadan** ölçer.

### 4.3 Punch olayı — yapısal yayın

Dış dünyadaki punch, `transactions` satırının **dışarıya bakan izdüşümüdür**.

- **Tablolar (hepsi API-2'de, RLS beşlisiyle):**
  - `punch_events(tenant_id, seq, punch_id, version, status, location_company_id, employee_company_id, created_at)` —
    `UNIQUE(tenant_id, seq)`; bileşik FK `(punch_id, tenant_id) → transactions`;
    `transactions` gibi UPDATE/DELETE/TRUNCATE tetikleyicileri; R3 tarayıcısı bu tabloyu da kapsar.
  - `feed_counters(tenant_id, last_seq)` — tenant başına tek satır (§4.4).
  - `webhook_endpoints`, `webhook_deliveries` — şema burada açılır, panel/gönderici API-9/10'da (§4.5).
- **Olayı veritabanı tetikleyicisi yazar** (binary sürümünden bağımsız — eski binary
  ve rollback da olay doğurur):
  - `AFTER INSERT ON transactions` — K-4 koşulu sağlanırsa;
  - `AFTER INSERT ON transaction_reviews` — `outcome='approved'` **ve** bağlı `transactions`
    satırı K-4'ün kanal/practice koşulunu sağlıyorsa.
  Saf `tap.Decide`, `policy` ve sayaç ilerletme yolu **dokunulmaz**.
- **Yetki modeli — `tappa_app` olay yazamaz:** tetikleyici fonksiyonları **SECURITY
  DEFINER**, sahibi yeni bir **NOLOGIN, NOBYPASSRLS** rol `tappa_feedwriter` (3. turda
  ölçüldü: definer içinde `current_setting('app.tenant_id')` çağıranın değerini verir,
  RLS `WITH CHECK` uygulanır, FORCE RLS altında sütun SELECT'i yeterli, `tappa_app`'in
  doğrudan INSERT'i 42501). Rolün yetkisi **tam olarak**:
  - `punch_events` INSERT; `webhook_deliveries` INSERT;
  - `feed_counters` **SELECT (tenant_id, last_seq)**, INSERT, UPDATE (last_seq) —
    `ON CONFLICT DO UPDATE … RETURNING` SELECT ister (3. tur: SELECT'siz ilk dokunuş 42501);
  - okuma sütunları: `transactions` (kanal, practice, verdict, lokasyon, çalışan, tenant),
    `locations.company_id`, `employees.company_id`, `webhook_endpoints` (kimlik, şirket,
    aktif), `tenants.api_enabled_at`.
  `tappa_app` bu üç tabloda §4.0 kuralına tabidir: `punch_events` SELECT;
  `feed_counters` SELECT; `webhook_deliveries` SELECT + yalnız kira/sonuç sütunlarına
  UPDATE (`lease_until`, `attempt`, `next_attempt_at`, `state`, `last_status`,
  `last_error_class`, `delivered_at`). INSERT/DELETE hiçbirinde yok.
- **Rolün yaşam döngüsü:** roller yalnız `scripts/db-init/01-roles.sql`'de doğar —
  `tappa_feedwriter` oradaki **idempotent** blokta (OP-5 kalıbı) ve canlı küme için
  `deploy/README.md` runbook'unda yaratılır; `pg_dump` rol taşımadığı için
  (`scripts/pg-restore-verify.sh:379`) geri yükleme provası **sıfırdan kurulmuş** bir
  pod'a yapılır. Migration'ın ön koşul bloğu rol yoksa **ya da üyesi varsa** düşer.
- **Katalog testi** (`internal/db/operatorschema_test.go:500-545` kalıbı, ADR 0021'e not):
  `tappa_feedwriter` NOLOGIN, NOBYPASSRLS, **üyesi 0**; sahip olduğu **her** fonksiyon
  `prorettype = trigger` (doğrudan çağrılabilir, `void` dönen bir yardımcı yazma kapısı
  olurdu) ve `proconfig`'i sabit (`search_path`, `lock_timeout`). ADR 0021'in "`prosecdef`
  sahibi yalnız {`tappa_resolver`, `tappa_opdefiner`}" testi `tappa_feedwriter` ile
  genişler — rol tenant sınırını **aşmaz** (politikalar PUBLIC, GUC'a bağlı), yalnız yazma
  hakkını tetikleyiciye kilitler.
- **Tetikleyicinin değişmezleri** (ADR 0028'de normatif, API-2'de testli):
  `SET search_path = pg_catalog, pg_temp` ve bütün adlar nitelikli; her sorguda açık
  `tenant_id = NEW.tenant_id` (kuşak + kemer) ve `NEW.tenant_id` = GUC **assert**'i;
  olayın şirket sütunları NOT NULL; hata yutan `EXCEPTION` bloğu **yok** (savepoint geri
  alımı kilidi bırakır — ölçüldü — ve olay sessizce düşerdi); uç nokta **adresini okumaz**
  (bozuk bir uç nokta tap yolunu kıramaz); ağ ya da uzun iş yok.
- **Eşlik eden listeler:** `punch_events` append-only olduğu için
  `cmd/tappa/scriptguards_test.go:447-510` (`APPEND_ONLY`), `scripts/pg-restore-verify.sh`
  (`trunc_tables` ve sabit "seven" başarı cümlesi) güncellenir; R5b'nin görmediği
  `CREATE TRIGGER` / `ALTER FUNCTION … OWNER` için katalog testi yeterli kapıdır.
- **Teslim satırı:** aynı tetikleyici, tenant'ın API erişimi açıksa (K-22), olay için
  eşleşen aktif uç noktalara `webhook_deliveries` satırı yazar. Ayrı yayılım taraması yok.
- **Geri doldurma (tek migration işlemi):** önce `transactions` **ve**
  `transaction_reviews` tabloları kilitlenir, iki tetikleyici kurulur, **sonra** geçmiş
  uygun satırlar için olay yazılır (sıra `occurred_at`, tenant sayaçları buna göre
  ilerletilir). Geri doldurulan olaylar için teslim satırı yazılmaz. Kilit süresi prod
  boyutunda bir kopyada **ölçülür**; **eşik 2 sn** — aşılırsa geri doldurma kendi
  sayaç kilidiyle tenant başına küçük işlemlere bölünür.
- **Bedel:** tetikleyicinin her hatası `ok` dokunuşun kaydını geri alır (sayaç ilerlemiş,
  kayıt yok — bugünkü her INSERT hatasıyla aynı sınıf). Başarısız kayıt kimlikleriyle
  loglanır (sır yok).

### 4.4 Akış imleci — neden kaybolmaz

Tetikleyici olayı yazmadan önce
`INSERT INTO feed_counters … ON CONFLICT (tenant_id) DO UPDATE SET last_seq = last_seq + 1 RETURNING last_seq`
çalıştırır. Satır kilidi commit'e kadar tutulur. Aynı tenant'ta iki olay-yazan işlem
sayacı **sırayla** artırır, yani `seq`'i sırayla alır ve **o sırayla commit eder**.
Bir okuyucu `seq = S`'ye kadar commit edilmiş olayları gördüğü anda, o tenant'ta
commit etmemiş olay en çok bir tanedir (satırı kilitleyen) ve `seq`'i `S`'den büyüktür;
sonrakiler daha da büyük. Dolayısıyla `WHERE tenant_id = $t AND seq > $cursor ORDER BY seq`
**hiçbir satırı atlamaz**. Abort eden işlem sayacı da geri alır (boşluk bile kalmaz).

- **Dizi yok → önbellek sorunu yok; sayaç tenant'a özgü → tenant'lar arası hacim sızmaz.**
  İmleç `seq`'i opak biçimde taşır; tenant ve kapsam filtresi her sorguda ayrıca.
- **Savepoint:** sayaç güncellemesi ile olay INSERT'i aynı alt işlemdedir; alt işlem
  geri alınırsa ikisi birlikte gider ve satır kilidi bırakılır — tutarlı. Tetikleyicide
  `EXCEPTION` bloğu yasağı (§4.3) bunu korur.
- **Kilit değişmezi:** sayaç satırı kilitlendikten sonra işlemde **çakışabilecek** hiçbir
  kilit alınmaz ve ağ/uzun iş yapılmaz (review yolunda ardından gelen `audit_log`
  INSERT'i ve teslim satırının FK'sinin aldığı KEY SHARE çakışmaz — 2. tur kodda doğruladı).
- **Takılı yazıcı (ölçüldü: tenant'ın 8 yazıcısının 8'i bekledi):** sayaç satırını
  tutup commit etmeyen tek bir işlem o tenant'ın bütün dokunuşlarını bekletir ve havuzu
  doldurarak diğer tenant'lara yayılabilir. Bu yüzden **API-2'nin ön koşulu:**
  `tappa_app` bağlantıları için `idle_in_transaction_session_timeout` ve TCP keepalive —
  **havuzun `RuntimeParams`'ıyla** verilir ve açılışta `SHOW` ile doğrulanır (`ALTER ROLE
  … SET` `pg_dump`'la taşınmaz, geri yüklemede sessizce kaybolurdu); tetikleyici
  fonksiyonun **özniteliği** olarak kısa bir `lock_timeout` taşır (`proconfig` — fonksiyon
  çıkışında eski değere döner; gövdede `set_config(…, true)` ise işlemin geri kalanına
  sızıyordu, 3. tur ölçtü). Aşılırsa dokunuş "tekrar dene" alır (sayaç ilerlemiş, kayıt
  yok — bugünkü sınıf; 3. turda ~505 ms'de 55P03 ölçüldü). Değerler ADR 0028'de.
- **Geri yükleme:** sayaçlar `pg_dump`'la yedek anındaki değerle gelir; yedekten sonra
  tüketilmiş olaylar varsa tüketicinin imleci bunların ötesindedir. **Uygulama yazmaya
  başlamadan önce** geri yükleme prosedürü `tenants`'taki **her** tenant için sayacı
  **upsert** ile büyük bir aralık ileri atar (yedek anında sayaç satırı olmayan tenant
  dahil — UPDATE onu atlardı); `scripts/pg-restore-verify.sh` (doğrulayıcı) her tenant için
  atlamanın **yapıldığını** denetler ve yapılmamışsa geri yüklemeyi reddeder (bugünkü
  ENABLE/FORCE sayım kapısının emsali); gece geri yükleme provası da atlatmayı koşar.
- Geçmiş sorgusu (`from`/`to`): `occurred_at` aralığı, `punch_events` üzerinden; ≤ 31 gün, sayfalı.

### 4.5 Webhook

- `webhook_endpoints(tenant_id, id, company_id, url, signing_key_sealed, active, created_at, rotated_at)` —
  `UNIQUE(tenant_id, company_id)` (şirket başına tek uç nokta); imza anahtarı
  `sun.Seal(TAPPA_WEBHOOK_KEK, AAD = endpoint id)` ile mühürlü (kendi KEK'i). İmza
  anahtarı **önekli** üretilir ki R7d değer kalıbıyla yakalayabilsin; rehberdeki
  bilinen-cevap vektörünün anahtarı **açıkça sahtedir** (CLAUDE.md §4.7).
- **URL kuralı:** `https`, port 443, IP literali yok, **userinfo yok, sorgu dizgesi yok,
  parça yok** (spec'in adresleri bu biçimde). URL ve `*url.Error` metni **asla** log'a ya
  da audit'e yazılmaz (Go o metinde yalnız parolayı siler, sorguyu korur); log ve audit
  yalnız uç nokta kimliği + hata sınıfı taşır.
- `webhook_deliveries(tenant_id, id, endpoint_id, punch_event_seq, attempt, next_attempt_at, lease_until, state, last_status, last_error_class, created_at, delivered_at)` —
  `UNIQUE(endpoint_id, punch_event_seq)`; `state ∈ pending|delivered|dead`;
  `last_error_class` **kapalı küme** (timeout · tls · refused · http_4xx · http_5xx ·
  blocked_address); uzak sunucunun yanıt metni saklanmaz, loglanmaz. `tappa_app`
  yalnız kira/sonuç sütunlarını UPDATE edebilir, INSERT edemez.
- **Yönlendirme (spec §4.5):** olayın kopyalanmış lokasyon şirketinin uç noktası ∪
  çalışan şirketinin uç noktası (aynıysa tek). Yeni uç nokta yalnız oluşturulduktan
  sonraki olayları alır.
- **Yük izin listesi:** spec §5 alanları + `channel`. IP, GPS, mesafe, not, policy
  bağlamı asla. Test ping sentetik (`event: "ping"`); ping **ve** panelden "yeniden
  gönder" aynı kovayı paylaşır: tenant başına saatte 10.
- **İşçi — kiralama modeli:** (1) vadesi gelen teslimi kısa bir işlemde
  `FOR UPDATE SKIP LOCKED` ile seç, `lease_until` yaz, commit; (2) HTTP isteğini işlem
  **dışında** gönder; (3) sonucu ayrı kısa işlemde yaz. Gönderim anında **ve** "yeniden
  gönder"de tenant'ın API erişimi (K-22) **ve uç noktanın `active`'i** yeniden kontrol
  edilir — devre dışı bırakılan uç noktaya kuyruktaki teslimler gitmez (K-26'nın
  savunma yazması gerçekten durdurur). Geri çekilme 1 dk · 5 dk · 30 dk · 2 sa · 6 sa →
  `dead`. Panelde teslim günlüğü + "yeniden gönder".
- **İşçi kurulumu — K-19, tenant'lar arası tarama yok:** süreç içi "işi olan tenant'lar"
  kümesi. Tenant kümeye o tenant'ta bir dokunuş/onay commit edildiğinde, kimliği
  doğrulanmış her API isteğinde ve panelden "yeniden gönder"de girer; işçi **`WithTenant`
  içinde** o tenant'ın vadesi gelmiş teslimlerini ve davet isteklerini okur. Pod yeniden
  başlarsa küme boştur (§2 madde 20).
- **SSRF — izin listesi:**
  - yalnız `https`, port 443, URL'de IP literali yok;
  - kontrol `net.Dialer.ControlContext` içinde, **gerçekten bağlanılan** adrese yapılır
    (her dial'da — DNS rebinding'i kapatır); adres `Unmap()` edilir ve:
    - **iki aile için aynı kural:** IANA **özel amaçlı adres kayıtlarının** (IPv4 ve IPv6)
      hiçbir girdisinde değil — liste kayıttan birebir alınır, elle seçilmez;
    - **IPv4** ayrıca küresel unicast (en az `0/8`, `10/8`, `100.64/10`, `127/8`,
      `169.254/16`, `172.16/12`, `192.0.0/24`, `192.0.2/24`, `192.88.99/24`,
      `192.168/16`, `198.18/15`, `198.51.100/24`, `203.0.113/24`, `224/4`, `240/4`);
    - **IPv6** ayrıca **yalnız `2000::/3`** içinden ve **`2001::/23`'ün tamamı** ile
      `3fff::/20` (RFC 9637), `2002::/16` düşülerek — `2001::/23` Teredo, PCP/TURN/SRP
      anycast (`2001:1::1`, `::2`, `::3` — en yakın, çoğu zaman yerel sunucuya gider),
      benchmarking `2001:2::/48`, ORCHIDv2 ve AMT'yi birlikte kapatır (3. tur `netip` ile
      ölçtü); IPv4-uyumlu, `fec0::/10`, `64:ff9b::/96` gibi biçimler `2000::/3` dışında
      kaldığı için reddedilir (Go'nun `IsGlobalUnicast`'ı bunlara `true` diyor — güvenilmez);
    - **düğümün kendi genel adresleri ve ingress adresi** değil — config listesi;
      liste **boşsa** prod ve sandbox **başlamaz**.
  - `Transport.Proxy: nil`; yönlendirme izlenmez; toplam 10 sn; `MaxResponseHeaderBytes`
    sınırlı; gövde okunmaz (en çok 1 KiB atılır); TLS doğrulaması varsayılan.
  - **Tehdit modeli ADR 0028'de:** URL'yi owner girer, tenant'ı herkes açabilir → K-22
    kapısı her yolda + ping/yeniden gönderim kovası + şirket başına tek uç nokta.
- **Kapanış bütçesi:** işçi `srv.Shutdown` ile **eşzamanlı** boşalır, 20 sn penceresinin
  içinde (bugün pay: 20 + 5 sn, `terminationGracePeriodSeconds: 30`, testin istediği
  ≥ 5 sn pay — `main.go:88`, `session.go:637`, `shutdownbudget_test.go:70`). Uçuştaki
  istek iptal edilir; kira dolunca yeniden denenir. Bunu sabitleyen test API-10'da.

### 4.6 Ortak sözleşme

- JSON hata gövdesi `{code, message, details}`; 400/401/403/404/409/422/429/503;
  429'da `Retry-After`. `limit` ≤ 1000 (varsayılan 500). İstek gövdesi ≤ 64 KiB.
- **Idempotency-Key** (yazma isteklerinde opsiyonel):
  `api_idempotency(tenant_id, key_id, idem_key, request_hash, status, result_ref, created_at)`,
  `UNIQUE(tenant_id, key_id, idem_key)`. Satır **iş işlemiyle aynı işlemde** yazılır:
  eşzamanlı ikinci istek UNIQUE indekste ilk commit edene kadar bekler, sonra kayıtlı
  sonucu döner; süreç ortada ölürse işlem geri alınır, takılı "işleniyor" kaydı kalmaz.
  Yanıt gövdesi saklanmaz (yalnız durum + kaydın kimliği). Aynı anahtar + farklı gövde → 422.
  24 sa sonra istek yolunda, o tenant için tembel silinir (tenant'lar arası temizlik yok).
- `GET /employees` yanıtı **e-posta içermez** (spec'te yok); `activationEmail` durumu ve zamanı içerir.
- OpenAPI 3.1 **elle yazılır**, embed edilir, `GET /v1/openapi.yaml` (Node yok, üretici yok).
- İmza için Tappa'nın kendi bilinen-cevap vektörü üretilir ve rehbere konur.

### 4.7 Askıya alma (ADR 0025 + K-26)

ADR 0025 askıyı yalnız bir **yazma kilidi** olarak tanımlar; okumalar, CSV ve kaydın
telafi yolları askıyı bilmez; panelde **deaktivasyon askıda kapalıdır** (ADR 0025 sınır 11).
API aynısını uygular; tek fark K-26'dır:

| Askıda açık | Askıda 403 `tenant_suspended` |
|---|---|
| bütün `GET`'ler, `/v1/punches`, `/v1/directory`, webhook gönderimi (kuyruktaki teslimlerin işçi tarafından gönderilmesi) · `PUT /employees` (**yeni** çalışan) · `resend-activation` · **K-26 savunma yazmaları:** API anahtarı iptali, uç nokta devre dışı bırakma (panel) | `PUT /locations` · var olan çalışanı değiştiren `PUT /employees` (**`active:false` dahil**) · `DELETE /employees` · anahtar oluşturma · uç nokta oluşturma/döndürme/**adres değiştirme** · **test ping** · panelden **"yeniden gönder"** · şirket kodu/adı düzenleme |

**Tabloda adı geçmeyen her M11 yazması askıda kapalıdır** (varsayılan kapalı).

Askı sütunları OP-15'e bağlı (bugün `tenants`'ta yok, OP-15 beklemede, migration numarası
00035 çakışıyor — OP-15 yeniden numaralanır). Bu yüzden **API-4 yalnız tek kontrol
noktasını** kurar; tablonun testleri **API-4b**'dir ve OP-15'e bağlıdır.

### 4.8 Sandbox (K-3)

- **Ayrı alan adı**, `taptime.mt`'nin alt alanı **değil** (`internal/handler/cookies.go:138-159` kısıtı).
- **Ayrı derleme:** simülasyon kodu **ayrı bir pakette**; o paketi yalnız `sandbox` build
  tag'li bir `cmd` dosyası import eder. Kapılar: prod derlemesi için `go list -deps`
  **negatif**, sandbox için **pozitif** kontrol (`packaging_test` kalıbı — sembol testi
  `-ldflags=-s -w` yüzünden boş geçerdi); CI `-tags sandbox` ile de vet/test koşar;
  deploy kapısı (`verify-image.sh` derleme bilgisini zaten okuyor — `:118`) imajın tag'ini
  doğrular. Sandbox ikilisi **yalnız** `TAPPA_ENV=sandbox` ile başlar (boş `TAPPA_ENV`'in
  dev'e düşmesi böylece kapanır); tag'siz ikili `sandbox` değerini **reddeder** (dev ve
  CI'daki kullanımı değişmez). Mevcut dev simülatörünün dört katlı kapısı **gevşetilmez**.
- **Sertleştirme:** K-28 (`IsHardened`) — sandbox her prod kapısında prod gibi davranır.
- **Sanal plaket izin listesi:** sandbox minter'ı ve sandbox çözümleyicisi yalnız
  ayrılmış, NXP olmayan bir sanal UID aralığını kabul eder (gerçek NXP UID'si `0x04` ile
  başlar); kontrol **her çözümlemede ve her eklemede** koşar (yalnız başlangıçta değil;
  `encoded_at`'a dayanılmaz — damgasız gerçek plaket meşru bir durum, 00025:150).
  Ortak KEK + prod satırı zinciri böylece yapısal olarak kapanır; ayrıca sandbox kendi
  sırlarıyla kurulur, prod yedeği oraya geri yüklenmez.
- **Simülasyon yüzeyi:** yalnız sandbox'ta `POST /v1/sandbox/taps {employeeRef, locationRef, channel}`.
- **Hesap açılışı:** sandbox kendi operatör yüzeyini kendi ops host'unda çalıştırır.
  `/signup` prod'daki gibi kalır (tenant açan bir `op_*` yok); API ve webhook ise K-22
  gereği **yalnız sandbox operatörü açınca** çalışır — kayıt açık olsa da operatörsüz
  hiçbir sandbox tenant'ı anahtar kullanamaz, uç nokta kuramaz.
- **E-posta:** gerçek e-posta yok; davet `panel` modunda, aktivasyon linki sandbox'a özel bir uçtan.
- **Altyapı:** ayrı namespace + Postgres; NetworkPolicy (prod'unki gibi kendi
  namespace'ini korur), Pod Security `restricted`, `ResourceQuota`/`LimitRange`.

### 4.9 Pasifleştirme bütçesi (K-25)

`DELETE /employees` ve `PUT {active:false}` aynı sınıftır ve **anahtar başına saatlik
bir bütçeyi** paylaşır (varsayılan 20; değer ADR 0028'de). Bütçe **bellekte tutulmaz**
(geri alınamaz bir eylemin tek freni yeniden başlatmada sıfırlanamaz): son bir saatin
audit satırlarından, **anahtar başına advisory kilit** altında sayılır
(`db/queries/invites.sql:403` emsali). Owner'ın birden çok anahtarı bütçeyi çarpar —
kabul edilmiş; sızmış tek anahtar saatte en çok 20 hak alır. Aşılınca 429 + `Retry-After`,
owner'a panelde görünür uyarı ve `api.deactivation_budget_exceeded` audit satırı. ADR
0010'a sapma notu: iki adımlı onay API'de yoktur, yerini bütçe alır.

---

## 5. Görevler

| ID | Görev | Boyut | Ajan | Bağımlılık |
|---|---|---|---|---|
| API-0 | ADR 0027 (şirket boyutu) + ADR 0028 (anahtar, punch yayını, sayaç imleci, tetikleyici değişmezleri, webhook, SSRF tehdit modeli, K-19 işçisi, sandbox kapısı, pasifleştirme bütçesi) + notlar: ADR 0026 (K-2), 0022 §7 (otomatik davet + kuyruk + şirket başına tavan), 0010 (K-25 + K-24 yan etkisi), 0025 (§4.7 + K-26), 0002 §7 (resolver 7 + `tenants` sütun yetkisi), 0021 (yeni `op_*` + `tappa_feedwriter` katalog testi) · CLAUDE.md §3 (yeni paketler), §7 (log yasakları) | M | yapıcı + üçüncü göz | — |
| API-1 | Şema: `companies` (+ `is_default`, signup'ta varsayılan şirket), `company_id`'ler (+ değişmezlik), `external_ref`, `employee_code`, `first_name`/`last_name`, `employees.updated_at` + K-21 tetikleyicisi, `invite_requests`, `locations.active`, K-14 CHECK, K-24 tekillikleri, `tenants.api_enabled_at` + açan/kapatan `op_*`, ikinci şirketi açan `op_*`. §4.0 yetki kuralı. Panel: şirket kodu/adı, şirket seçimi, "External ref" alanları, manuel kayıtta lokasyon seçimi | L | `tappa-db-migrator` + yapıcı (`tappa-brand`) | API-0 |
| API-2 | `punch_events`, `feed_counters`, `webhook_endpoints`, `webhook_deliveries` şeması · `tappa_feedwriter` (`01-roles.sql` idempotent blok + canlı küme runbook'u) + katalog testi + iki tetikleyici (olay + teslim satırı) · geri doldurma · `tappa_app` zaman aşımları (`RuntimeParams`) · geri yükleme sayaç atlatma + doğrulayıcı kapısı + eşlik eden listeler | M | `tappa-db-migrator` | API-1 |
| API-3 | `api_keys` + resolver + `internal/apikey` (redakte tip) + panel (owner) + audit + R7/R7b/R7d kalıpları + env anahtarlarının çift çift ayrımı | M | yapıcı (`tappa-brand`) | API-1 |
| API-4 | `/v1` iskeleti: host kapısı (genelleştirilmiş), biçim kontrolü + IP kovası + Bearer + anahtar kovası, JSON hata, gövde sınırı, Idempotency-Key, askı **kontrol noktası**, `openapi.yaml` iskeleti, K-19 işçi iskeleti (küme + kiralama + kapanış) | M | yapıcı | API-3 |
| API-4b | Askı tablosunun (§4.7) testleri | S | yapıcı | API-4, **OP-15** |
| API-5 | `PUT /v1/locations/{ref}` (K-7, K-20, K-24) | S | yapıcı | API-4 |
| API-6 | `PUT/GET/DELETE /v1/employees/{ref}`, `resend-activation`; K-14…K-18, K-24, K-25; davet kuyruğu işçisi | L | yapıcı | API-4 |
| API-7 | `GET /v1/punches` (iki kapsam, imleç, `from/to`) | M | yapıcı | API-2, API-4 |
| API-8 | `GET /v1/directory?updatedSince` (K-21) | S | yapıcı | API-1, API-4 |
| API-9 | Uç nokta paneli (adres, oluştur/döndür — bir kez göster, devre dışı bırak, test ping) + SSRF-güvenli istemci | M | yapıcı (`tappa-brand`) | API-2, API-4 |
| API-10 | Webhook gönderici: kiralama, geri çekilme, `dead`, yeniden gönder, kapanış testi, panel teslim günlüğü | L | yapıcı (`tappa-brand`) | API-9 |
| API-11 | Sandbox ikilisi + simülasyon paketi + kurulum (§4.8) | M | yapıcı + kullanıcı (alan adı/DNS) | API-5, API-6, API-7, API-8, API-10 |
| API-12 | OpenAPI tamamı + entegrasyon rehberi (EN; §2'nin 24 maddesi + imza vektörü) | S | yapıcı | API-5…API-11 |
| API-13 | Canlıya alma: sırlar, `api.` DNS + Ingress, **`tappa-security-auditor` tam tur**, operatörden K-22 açılışı, KF bağlama runbook'u (K-18), KM şirketi + KM plaketleri (§3 — kullanıcıya sorulur), eski KM tenant'ının kapatılması, prod anahtar çifti eşitlik ölçümü (değer basmadan), spec §7 kabul listesi | M | yapıcı + denetçi + kullanıcı | hepsi + **EM-12** + **OP-15** |

Her görev: yapıcı (opus) → **ayrı** üçüncü göz → bulgu varsa düzelt + yeniden
denetle (CLAUDE.md §10). Büyük testler (`-race` tamamı, `make check`) yalnız görev
sonunda bir kez. Dal: `m11-api` (`main`'e birleştirme = deploy kararı, kullanıcının).
Her commit'ten önce `./scripts/redline-check.sh` — pre-push kancası push edilen
**her** commit'i tarar, sonradan silmek yetmez.

### Kabul çekirdeği (kart açılırken genişler — burada bağlayıcı)

- **API-0:** her ADR notu var; CLAUDE.md §4.5 metni **değişmemiş** (K-19); ADR 0028'de SSRF tehdit modeli, tetikleyici değişmezleri ve sandbox kapısı normatif.
- **API-1:** `\d` ile her yeni tablo RLS beşlisi + GRANT; **§4.0: CI, prod'un `arwd` varsayılanını kurup her yeni tabloda dört fiili ölçer**; owner ikinci şirket **açamaz**, `op_*` açar; mevcut her tenant'ın tam bir varsayılan şirketi var; **`/signup` ile açılan yeni tenant'ın da**; K-24 indeksleri ve **kısıt adı korunmuş** (panelde "adres alınmış" hâlâ 409, 500 değil); pasif satırla aynı e-postada yeni aktif satır açılıyor, iki aktif satır açılamıyor; `employees.company_id` UPDATE'i reddediliyor; `location_id` okuyucu sayımı rapora yazılmış; `updated_at` dört UPDATE yolunda ve **yalnız** rehbere görünen sütun değişince değişiyor (her biri için test); `api_enabled_at`'ı `tappa_app` yazamıyor, `op_*` açıp kapatabiliyor.
- **API-2:** (a) olay kodunu bilmeyen düz bir `INSERT INTO transactions` olay doğurur; (b) **çok oturumlu stres testi** (≥ 12 yazıcı, ≥ 3 tenant, INSERT ile COMMIT arasında rastgele 0–30 ms, abort ve savepoint geri alımı karışık, eşzamanlı okuyucu) → **sıfır** kaçan ve `seq` yoğun; **iki mutasyonda kırmızı**: kilitsiz dizi, ayrı işlemde artırılan sayaç (3. tur ikisini de ölçtü: 843 / 408); (c) geri yüklenmiş kopyada doğrulayıcı, sayaç atlatılmadan geri yüklemeyi reddeder; atlatılınca yeni olay eski imlecin ötesinde döner; (d) flag yazan dokunuş olay yazmaz, onaylanınca yazar, reddedilince yazmaz; manuel ve `practice` satırı (onaylı `practice` flag dahil) asla; (e) B tenant'ı A'nın olaylarını görmez (filtresiz sorgu); (f) `arwd` varsayılanlı veritabanında `tappa_app` ile `punch_events`/`feed_counters` üzerinde INSERT, UPDATE, DELETE **42501**; `webhook_deliveries` üzerinde INSERT, DELETE ve kira/sonuç dışı sütun UPDATE'i 42501; (g) tetikleyici hatası dokunuş kaydını geri alır; (h) savepoint içinde geri alınan olay sayacı da geri alır; (i) takılı yazıcı `lock_timeout` sonunda bırakılır ve tetikleyiciden sonra işlemin `lock_timeout`'u eski değerindedir (öznitelik sızmaz); `tappa_app` bağlantısında `SHOW idle_in_transaction_session_timeout` beklenen değer; (j) olay satırındaki şirket kimlikleri lokasyonun şirketi sonradan değişse de sabit; (k) kapalı API erişiminde olay yazılır, teslim satırı yazılmaz; (l) geri doldurmanın kilit süresi prod boyutunda bir kopyada ölçülmüş ve **≤ 2 sn** (aşarsa bölünmüş hâli); (m) katalog testi: `tappa_feedwriter` NOLOGIN, NOBYPASSRLS, üyesiz, sahip olduğu her fonksiyon `prorettype = trigger`, `proconfig` sabit; (n) yedek **sıfırdan kurulmuş** bir pod'a geri yüklenir (rol `01-roles.sql`'den gelir) ve doğrulayıcı geçer; rolün bir üyesi varken migration düşer.
- **API-3:** iptal edilmiş anahtar bir sonraki istekte **401**; `api_enabled_at` boş tenant'ın anahtarı **403**; yanlış ortam öneki DB'ye gitmeden 401; anahtar log'da/hata metninde/audit'te yok; yeni R7/R7b/R7d kökleri yeşil ve **kasıtlı ihlalde kırmızı**; `TAPPA_WEBHOOK_KEK` = `TAPPA_TAG_KEK` (ya da başka herhangi bir anahtar) yapıştırılınca süreç başlamaz; B'nin anahtarı A'nın tek satırını okuyamaz/yazamaz.
- **API-4:** API host'unda `/admin` 404, ana host'ta `/v1` 404; geçersiz anahtar selinde IP kovası resolver'dan önce 429; aynı Idempotency-Key + farklı gövde 422; eşzamanlı aynı anahtar tek işlem, ikinci istek kayıtlı sonucu döner; süreç işlem ortasında ölünce anahtar takılı kalmaz.
- **API-4b:** §4.7 tablosunun her hücresi için bir test.
- **API-5:** KM anahtarı KF lokasyonunu okuyamaz/değiştiremez (aynı `externalRef` ile bile yeni KM satırı açar); farklı `timezone` 422; tekrar `PUT` yeni satır açmaz.
- **API-6:** KM anahtarı KF çalışanını okuyamaz/değiştiremez/pasifleştiremez; aktif bir KF çalışanının e-postasıyla KM'de çalışan açmak **409 `email_unavailable`** (yeni satır açılmaz, davet gitmez); pasif çalışana `active:true` → yeni satır, eski geçmiş yerinde; e-posta değişince eski davet aynı işlemde ölü; `resend-activation` döngüsü canlı daveti öldürmez (N çağrı → en çok bir gönderim); aktive çalışana `resend-activation` 409; KM'nin davet seli KF'nin şirket tavanını tüketmez ve **KF'nin panelden davetini reddettirmez** (panel ve kuyruk aynı `(tenant, şirket)` sayacı); tek şirketli bir tenant süreç kesicisinden en çok 50/300 alır; kuyrukta **kod yok** (şema + log taraması); iki pod aynı daveti bir kez basar; pasifleştirme bütçesi aşımında 429 + owner uyarısı + audit, ve bütçe **süreç yeniden başlayınca sıfırlanmaz** (DB'den sayılır); `GET` yanıtında e-posta yok.
- **API-7:** dört yönlendirme şeklinin akış karşılığı: KF@KF yalnız KF'ye, KM@KM yalnız KM'ye, KM@KF hem KF (`location`) hem KM (`employer`), KF@KM hem KM hem KF; manuel ve reddedilmiş satır hiçbir kapsamda yok; `limit` 1001 → 400.
- **API-8:** `updatedSince` örtüşme penceresi içindeki değişikliği kaçırmaz; yanıtta e-posta yok; davet kirası `updated_at`'ı değiştirmez.
- **API-9:** uç nokta imza anahtarı ikinci kez gösterilemez; `op_*` fonksiyonları `signing_key_sealed` ve `token_hash` sütunlarını göremez; ping ve yeniden gönderim aynı kovada; kapalı API erişiminde uç nokta oluşturma/ping/yeniden gönderim reddedilir; şirket başına ikinci uç nokta açılamaz.
- **API-10:** **dört yönlendirme şekli** (KF@KF → 1 teslim KF'ye; KM@KM → 1 teslim KM'ye; KM@KF → 2 teslim; KF@KM → 2 teslim); `169.254.169.254`, `0.0.0.0`, `::ffff:127.0.0.1`, `::7f00:1`, `64:ff9b::` + iç adres, `2001::1`, `2001:1::1`, `2001:2::1`, `3fff::1`, düğüm adresi, `localhost`'a çözülen ad, `http://`, userinfo'lu ya da sorgulu URL, 302 → reddedilir; **iki aşamalı DNS** (ilk çözüm genel adres, ikinci iç adres) bağlantı anında reddedilir; devre dışı bırakılan uç noktaya kuyruktaki teslim gitmez; URL ve `*url.Error` metni log'da ve audit'te yok; düğüm listesi boşken süreç başlamaz; `HTTP_PROXY` tanımlıyken bile doğrudan bağlanır; aynı teslim iki pod'dan bir kez gider; gönderim sırasında açık DB işlemi yok; API erişimi kapatılınca bekleyen teslim gönderilmez; imza kendi vektörümüzle doğrulanır; kapanış bütçesi testi yeşil.
- **API-11:** prod derlemesinde simülasyon paketi `go list -deps`'te yok, sandbox'ta var; CI `-tags sandbox` vet/test; deploy kapısı yanlış tag'li imajı reddeder; sandbox ikilisi `0x04` önekli (gerçek) UID'yi minter'da da çözümleyicide de reddeder; tag'siz ikili `TAPPA_ENV=sandbox`'ı reddeder, sandbox ikilisi boş ya da başka `TAPPA_ENV` ile başlamaz; K-28'in **dokuz kapısının her biri** sandbox ortamında sınanır (ayrıcalıklı DB rolü reddi dahil); operatör açmadan sandbox tenant'ının anahtarı 403.
- **API-12:** OpenAPI her uç ve hata kodunu kapsar; §2'nin 24 maddesi rehberde.
- **API-13:** spec §7 listesinin dokuz maddesi tek tek işaretli — 6. madde ("düzeltme ve iptal yeni `version`") **sözleşme farkı** olarak (§2 madde 3); güvenlik denetimi ONAY.

---

## 6. Riskler ve tuzaklar (ölçülmüş)

- **R7 log tarayıcısı** `token|secret|…` geçen her log çağrısını düşürür; R7d plan/doküman satırlarını da tarar (bu dosyanın 1. sürümü yakalandı ve push edilemedi).
- **Tek replika, bellekte limitler ve K-19 kümesi:** süreç başına; rollout'ta 2× patlama mümkün (T1); işçi kümesi yeniden başlamada boşalır (§2 madde 20).
- **Takılı yazıcı tenant'ı durdurur** (§4.4) — zaman aşımları API-2'nin ön koşulu, sertleştirme adayı değil.
- **Davet tavanları ve 300/saat süreç kesicisi** ilk toplu senkronu saatlere yayar — runbook'ta yazılır, KF-RMS ekibine önceden söylenir.
- **Pilot verisinin çiftlenmesi** → K-18; API-13 runbook'u ilk `PUT`'tan önce panel eşlemesini ister.
- **K-1 bedelleri** (§3) ve iki **canlı ön koşul**: **EM-12** (KM yöneticileri) ve **OP-15** (askı sütunları — API-4b ona bağlı). EM-12 şirket kapsamlı yetki getirirse K-24'ün "adres kullanımda" artık riski (409 kehaneti, adres işgali) yeniden değerlendirilir — EM-12 kartına not.
- **GDPR:** rehber iki şirketin çalışan adlarını karşılıklı açar (spec istiyor); DPA/aydınlatma metni güncellemesi kullanıcıda (Q13 ile birlikte).
- **Egress açık:** SSRF kapısı tek savunma; egress NetworkPolicy backlog adayı.
- **Pod ağında IPv6 olup olmadığı doğrulanmadı** — yoksa IPv6 sınıfları bugün sömürülemez, kapı yine yazılır.

## 7. Kullanıcının dış adımları

1. ~~K-1, K-2, K-3, K-19, K-25, K-26~~ ✅
2. Sandbox için `taptime.mt` dışında bir alan adı + DNS; `api.taptime.mt` DNS kaydı (Cloudflare, DNS-only).
3. KF-RMS / KM-ERP ekibinden canlı + test webhook adresleri (spec §8).
4. Canlı öncesi: paneldeki KF lokasyon ve çalışanlarının bağlanması (K-18); KF tenant'ının varsayılan şirket kodunun `KF` yapılması.
5. KM plaketlerinin akıbeti (taşıma mı, yeni plaket mi) — API-13 öncesi sorulacak.
6. Eski KM tenant'ının kapatılması ve KM şirketinin açılması (operatör).
7. Canlı ön koşullar: **EM-12** ve **OP-15**'in tamamlanması.

## 8. Kapsam dışı (v1)

Punch yazma API'si (spec §9) · punch iptali/düzeltmesi · departman uçları · lokasyon
silme · webhook yönetimi API'den · lokasyon başına saat dilimi · şirket başına marka ·
şirket kapsamlı panel yetkisi (EM-12'nin işi) · OAuth · çoklu replika için paylaşımlı
oran deposu · egress NetworkPolicy.

---

## 9. Denetim izi

### 1. tur (2026-10-09, `a169391`) — üçüncü göz RED, güvenlik RED

| Bulgu | Kaynak | Nereye işlendi (3. sürüm) |
|---|---|---|
| Tenant'lar arası tarama yeni CLAUDE.md §4.5 istisnası | üçüncü göz B-1, güvenlik B1 | K-19 (kullanıcı) · §4.5 · §4.6 — 2. turda **kapandı** onaylandı |
| Olay Go katmanında → deploy/rollback'te kayıp | üçüncü göz B-2 | §4.3 tetikleyici · API-2 (a) |
| `xid8` imleci geri yüklemede atlıyor | üçüncü göz B-3 | K-13 sayaç satırı · §4.4 · API-2 (b)(c) |
| Askı ADR 0025 ile çelişiyor | üçüncü göz B-4, güvenlik O7 | §4.7 (3. sürümde yeniden hizalandı) |
| Sandbox kapısı / alt alan çerez kısıtı | güvenlik B2, üçüncü göz O-2 | K-3 · §4.8 · K-28 |
| Şirket sınırı, e-posta kehaneti, pasif satır | güvenlik O1, üçüncü göz O-5 | K-24 (3. sürümde yeniden) · API-5/6 |
| KM plaketi seçenekleri | güvenlik O2, üçüncü göz O-3 | §3 — 2. turda kapandı |
| Kimliksiz istek sınırı | güvenlik O3 | §4.2 — 2. turda kapandı |
| SSRF yasak listesi | güvenlik O4 | §4.5 izin listesi (3. sürümde IPv6 `2000::/3`) · K-22 her yolda |
| Eski davet yaşıyor; aktive çalışana resend | güvenlik O5, üçüncü göz D-6 | K-15 · K-17 |
| İmza anahtarı "göster"; imzasız zaman damgası | güvenlik O6 | K-11 · K-12 |
| Kapanış bütçesi, K-1 bedelleri, `updated_at`, `active:false`, ADR notları | üçüncü göz O-1, O-4, O-6, O-7, O-8 | §4.5 · §3 · K-21 · K-20 · API-0 |
| D1–D9, D-1…D-11 | ikisi | §4.2–4.6 · §2 · K-10 · K-23 |

### 2. tur (2026-10-09/10, `e0ad91f`) — üçüncü göz RED, güvenlik RED

| Bulgu | Kaynak | Nereye işlendi |
|---|---|---|
| İspat dizinin `CACHE 1` olmasına dayanıyor (ölçüldü: `CACHE 20` ile 1234'ün 1016'sı kaçtı); API-2 (b) ihlali yakalayamaz | üçüncü göz B1 | K-13: **dizi yok**, tenant sayaç satırı · API-2 (b) çok oturumlu stres + mutasyon |
| Askıda `DELETE` açık bırakılmış → ADR 0025'in kullanıcı onaylı sınırını tersine çeviriyordu; askıda sızmış anahtar iptal edilemiyordu; `PUT active:false` ile `DELETE` farklı sınıfta | üçüncü göz B2 | §4.7 yeniden hizalandı · K-26 (kullanıcı) · K-22 kapatma yolu |
| K-24 + tenant-ortak tavan → KM, KF çalışanını etkileyebiliyor | güvenlik ORTA-1 | K-24 e-posta tenant geneli (pasif hariç) · K-17 şirket başına tavan · API-6 kabul |
| `resend` daveti istek anında öldürüyor | güvenlik ORTA-2 | K-17 · §2 madde 12 |
| K-22 yalnız anahtar yolunda | güvenlik ORTA-3 | K-22 her yolda · API-9/10 kabul · §4.8 hesap açılışı |
| `TAPPA_ENV=sandbox` sertleştirmeleri kapatıyor | güvenlik ORTA-4, üçüncü göz O6 | K-28 · API-11 kabul |
| `tappa_app` olay yazabiliyor; tetikleyici yetki modeli tanımsız | güvenlik ORTA-5, üçüncü göz O1 | §4.3 `tappa_feedwriter` + REVOKE · API-2 (f) |
| Takılı yazıcı tenant'ı durduruyor (ölçüldü 8/8) | güvenlik ORTA-6, üçüncü göz O9 | §4.4 zaman aşımları API-2 ön koşulu · API-2 (i) |
| API `DELETE` ADR 0010 onayını atlıyor | güvenlik ORTA-7 | K-25 (kullanıcı) · §4.9 |
| Şirket aidiyeti olay anında dondurulmuyor | üçüncü göz O2, güvenlik D7 | K-27 · API-2 (j) |
| Teslim satırı şeması görev sırasında ters; yönlendirme kuralının kabulü yok | üçüncü göz O3 | webhook şeması API-2'de · API-7/API-10 dört şekil |
| 401/403 çelişkisi; resolver'ın `tenants` yetkisi adsız | üçüncü göz O4, güvenlik D3 | §4.2 "taşı, uygulama" · ADR 0002 notu |
| API-4 kabulü olmayan şemaya dayanıyor | üçüncü göz O5 | API-4 kontrol noktası · API-4b ← OP-15 |
| `encoded_at` bekçisi yapısal değil | üçüncü göz O7, güvenlik D6 | §4.8 sanal UID izin listesi her çözümlemede |
| K-19 bedeli sözleşmede yok | üçüncü göz O8 | §2 madde 20 |
| R7 imza anahtarı adını görmüyor; R7b ad alanları; anahtar ayrımı | güvenlik D1, D2 | §4.2 · API-3 kabul |
| SSRF IPv6 sınıfları, boş düğüm listesi, yeniden gönderim kovası, uç nokta sayısı | güvenlik D4 | §4.5 |
| Küresel `seq` tenant'lar arası hacim sızdırıyor | güvenlik D5, üçüncü göz D5 | K-13 tenant sayacı |
| Sandbox altyapısı; build tag testi boş geçer | güvenlik D6, üçüncü göz D10 | §4.8 · API-11 kabul |
| `GET /employees` e-posta; kira süresi | güvenlik D8 | §4.6 · K-17 |
| "Son kilit" ifadesi; `EXCEPTION` bloğu; geri doldurma sırası; geri yükleme sırası ve doğrulayıcı | üçüncü göz D1–D4 | §4.3 · §4.4 |
| E-posta indeksi kısıt adı ve tek şirketli yan etki; yeni tenant'ta varsayılan şirket; kira şeması; Idempotency takılması; review tetikleyicisi K-4; EM-5B durumu; atıf kuralı; §2 eksikleri | üçüncü göz D6–D9, D11–D14 | K-24 · §4.1 · K-17 · §4.6 · K-4 · §1 · başlık · §2 madde 14/21 |

### 3. tur (2026-10-10, `41c956d`) — üçüncü göz RED, güvenlik RED — **bloklayan yok**

İmleç tasarımı gerçek Postgres'te ölçüldü ve doğrulandı (3 koşu, 0 kaçan; iki mutasyon
kırmızı); definer + GUC + FORCE RLS davranışı ölçüldü.

| Bulgu | Kaynak | Nereye işlendi (4. sürüm) |
|---|---|---|
| Yalnız INSERT REVOKE; prod `arwd` varsayılanında UPDATE/DELETE kalıyor (sayaç sıfırlanınca tenant'ın bütün dokunuşları kayıtsız) | güvenlik ORTA-1, üçüncü göz ORTA-1 | §4.0 ortak şema kuralı · §4.3 tam yetki listesi · API-1/API-2 (f) `arwd`'li CI ölçümü |
| `tappa_feedwriter` yetki seti çalışmıyor (`feed_counters` SELECT eksik, ölçüldü 42501); rolün db-init/runbook/geri yükleme yaşam döngüsü ve üyelik sabitlemesi yok | üçüncü göz ORTA-1, güvenlik D-1 | §4.3 yetki listesi, yaşam döngüsü, katalog testi · API-2 (m)(n) |
| Şirket başına tavan: panel yolu tanımsız; şirket sayısı sınırsız → tek kayıt süreç kesicisini doldurur | güvenlik ORTA-2, üçüncü göz ORTA-2 | K-17 (panel + kuyruk aynı sayaç) · §4.1 ikinci şirketi yalnız operatör açar · API-6 kabul |
| K-13 ↔ §4.4 metin çelişkisi | üçüncü göz D1 | K-13 |
| `lock_timeout` gövdede sızıyor; zaman aşımı mekanizması | üçüncü göz D2, D3, güvenlik D-2 | §4.4 öznitelik + `RuntimeParams` · API-2 (i) |
| Geri yüklemede sayaç atlatma eksik | üçüncü göz D4 | §4.4 her tenant için upsert + gece provası |
| Yalın R7 kökleri ağacı kırmızıya çevirir (35 satır) | üçüncü göz D5 | §4.2 tarayıcı eklemeleri önce sayılır, değer biçimli kalıplar |
| §4.7 eksik hücreler | üçüncü göz D6 | §4.7 tablo + "varsayılan kapalı" |
| §4.8 ↔ API-11 başlatma kuralı | üçüncü göz D7 | §4.8 |
| Pasifleştirme bütçesinin deposu | üçüncü göz D8, güvenlik D-8 | §4.9 DB'den, anahtar başına kilit |
| K-28 kapıları adsız | üçüncü göz D9 | K-28 dokuz yer · API-11 |
| Geri doldurma eşiği; API-12 → API-11; OP-15 canlı ön koşul | üçüncü göz D10–D12 | §4.3 (2 sn) · §5 · §6 · §7 |
| K-14 bedeli: "farklı şube" notu düşmez | üçüncü göz D13 | K-14 · §2 madde 24 |
| Spec §7 madde 6 sapma; 120 sn varsayılan | üçüncü göz D14 | API-13 · §2 madde 5 |
| Devre dışı uç noktaya teslim sürüyor | güvenlik D-3 | §4.5 işçi · API-10 |
| IPv6 izin listesinde artık (`2001:1::1` vb.); DNS rebinding mekanizması | güvenlik D-4 | §4.5 `2001::/23` + `3fff::/20`, `ControlContext` · API-10 |
| URL userinfo/sorgu; URL ve hata metni log'u; imza anahtarı öneki; sahte vektör | güvenlik D-5 | §4.5 URL kuralı · §4.2 · API-10 |
| Sandbox hesap açılışı mekanizması | güvenlik D-6 | §4.8 |
| K-24 artık riski EM-12'ye şartlı | güvenlik D-7 | §6 EM-12 notu |
| Anahtar ayrımının genelleştirilmesi prod açılışını reddettirebilir | güvenlik D-9 | §4.2 · API-13 runbook |
| `delivered`→`pending` | güvenlik D-10 | değişiklik gerekmedi (kova + tüketici tekilleştirmesi) |
