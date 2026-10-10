# M11 — Entegrasyon API'si (KF-RMS · KM-ERP)

> **Durum:** PLAN, **11. sürüm** (2026-10-10, 28. oturum). Kod yok. 10. turda (`e243e5b`) iki
> mercek RED verdi; ikisi de aynı gerilemeyi ölçtü: K-1e'nin yerine konan "KF raporu izdüşüm
> içinde eşler" kuralı, KF ↔ KM arasında dönen bir çalışan için KF'de hiç geçmemiş aralıklar
> sayıyordu (9 sa / 32 sa). K-1e'nin kaldırılmasıyla kayıt kaybı **kapandı** (5/5 senaryo
> yazıldı), `holding_peer` kâhini kapandı, mutantlar tetikleyici yolunda da kırmızı. Bu sürüm
> 10. turun bulgularını işler — §9.
>
> *10. sürüm notu:* 9. turda (`bea8ac3`) iki
> mercek RED verdi; en ağır bulguyu ikisi de ölçtü: K-1e'nin kapanış bağı, geriye beyanlı bir
> çıkışı ve geriye dönük manuel düzeltmeyi **her tenant'ta** yazılamaz kılıyordu (CLAUDE.md
> §4.6). 7.–9. turların ORTA'larının çoğu K-1e'den doğduğu için kullanıcı **K-1e'yi kaldırdı**
> (2026-10-10). Bu sürüm onu ve 9. turun kalan bulgularını işler — §9.
>
> *9. sürüm notu:* 8. turda (`62519e5`) iki
> mercek RED verdi (3 + 2 ORTA), **bloklayan yok** (üçüncü tur üst üste); ölçüldü: "var et"
> adımı eksik sayaç satırında 8 640 işlemde 0 kilitlenme (mutant 324); üç sonuçlu ilerletme
> tek ifadede, 20 eşzamanlı çağrıda tam 1 `advanced`; K-29 kanal × zaman modeli çekirdekte
> doğru. Bu sürüm 8. turun bulgularını işler — §9.
>
> *8. sürüm notu:* 7. turda (`71b8dc6`) iki
> mercek de RED verdi ama **bloklayan yok**; K-31 tek ön-kilidi 14 288 işlemlik stresle (8 106
> holding) **0 kilitlenme, 0 kaçan** ölçüldü; holding fonksiyonları prototipte kurulup ölçüldü
> (iki yönlü süzgeç, 18 sütunluk "asla SELECT", logo kapısı, tetikleyici bağlama reddi). Bu
> sürüm 7. turun bulgularını ve kullanıcının K-1h kararını işler — eşleme §9'da.
>
> *Önceki sürüm notu (7.):*
> 1.–5. sürüm "tek tenant, iki şirket" modeliydi (5. sürüm iki mercekten ONAY). Kullanıcı
> modeli değiştirdi: **KF ve KM aynı holding altında ayrı iki tenant; holding bağıyla çapraz
> dokunuş**. 6. sürüm (`77bd943`) bu modeli kurdu; 6. turda iki mercek de RED verdi — en
> ağırı **ikisinin de ölçtüğü** kilitlenme (sayaçlar iki ayrı tetikleyiciye bölününce ters
> yönlü iki holding dokunuşundan biri geri alınıyordu). Bu sürüm 6. turun bütün bulgularını ve
> kullanıcının dört yeni kararını (K-1e, K-1f, K-1g, K-3'ün geri dönüşü) işler — eşleme §9'da.
> **Sıradaki adım: 11. tur denetim (iki mercek), ONAY gelirse API-0.**
>
> **Kaynak:** kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve KM-ERP
> entegrasyonu)"*, 2026-10-09. **Repoya konmadı** — depo public ve doküman müşterinin iç
> webhook adresini taşıyor. "spec §N" o dokümanın bölümüdür.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"**; çıplak "§N" bu planın bölümüdür.

**Amaç.** Tenant'ın panelinden ürettiği **API anahtarlarıyla** dış İK/ERP sistemlerinin
(KF-RMS → KF tenant'ı, KM-ERP → KM tenant'ı) Tappa'ya lokasyon ve çalışan göndermesi,
punch'ları **imleçli akıştan** çekmesi ve **webhook** ile anında alması; ve aynı holding'deki
tenant'ların çalışanlarının birbirinin plaketlerinde okutabilmesi.

**Neden ayrı milestone.** Bugün dış dünyaya açık JSON API, Bearer kimliği, webhook, OpenAPI,
Idempotency-Key yok; tenant'lar arası dokunuş `sys:tenant-mismatch` ile reddediliyor (ölçüldü).
Bu iş yeni bir **güvenlik sınırı**, yeni bir **giden ağ yüzeyi**, yeni bir **sıralama
sözleşmesi** ve CLAUDE.md §4.5'e **ikinci bilinçli istisna** açar — hepsi ADR ister.

---

## 1. Spec ↔ Tappa — bugünkü durum (ölçüldü)

| Spec beklentisi | Tappa bugün | Plan |
|---|---|---|
| İki şirket (KF/KM), **her çalışan iki şirketin her lokasyonunda okutabilir** | KF ve KM **iki ayrı tenant**; yabancı tenant plaketi → `sys:tenant-mismatch` → **403, kayıt yok** (`internal/policy/guardrails.go:306-319`, `internal/handler/checkin.go:227-233`) | **K-1** holding bağı (§4.1) |
| Sistem başına API anahtarı; punch'ta `company` (`KF`/`KM`) | Yok | Anahtar = tenant; `company` = operatörün yazdığı tenant kodu (K-22) |
| Location: `externalRef`, `timezone`, `active` | external id yok · saat dilimi **tenant** sütunu (owner panelden değiştirebiliyor — `db/queries/tenants.sql:158-164`) · aktif bayrağı yok, silme sert `DELETE` | API-1, K-7, K-20, K-36 |
| Tag panelden lokasyona bağlanır | Var (`tags.uid` küresel PK). `unassigned` plaketin `location_id`'si NULL (00013:338-339). QR satırları da `tag_uid` taşır (`internal/domain/checkin/checkin.go:1282-1285`) | `tagId = tag_<uid>`; K-30 |
| Employee: `externalRef`, `employeeCode`, `firstName`, `lastName`, `email`, `active`, `activated` | external id/kod yok · tek `full_name` · `employees_tenant_email_key (tenant_id, email) WHERE email IS NOT NULL` — pasif satırlar dahil; kod çakışmayı kısıt adıyla tanıyor (`internal/domain/tenant/staff.go:706-717`) · `status` + `activated_at` · `location_id NOT NULL` · `updated_at` yok | API-1, K-14, K-21, K-24 |
| Yeni çalışana **otomatik** aktivasyon e-postası | Çalışan eklemek davet göndermez; davet senkron. Tavanlar: tenant 50/saat 300/gün · çalışan 3/saat · kutu 5/saat 20/gün · süreç kesicisi 300/saat. **EM-5B canlıda** | API-6, K-16, K-17 |
| **İlk dokunuş = ilk punch** | ADR 0026: aktivasyon dokunuşu kayıt yazmaz | **K-2** |
| "Doğrulanamayan dokunuş punch'a dönüşmez" | `flag` yazılır, onay kuyruğuna düşer (CLAUDE.md §4.6) | K-4 |
| Düzeltme/iptal → yeni `version` | Geçerli kaydı iptal yolu yok (ADR 0011) | K-5 |
| Manuel punch'lar RMS/ERP'de (spec §9) | Tappa'da `channel='manual'` var | K-6 |
| İmleçli akış, kayıt sırasına göre, geç gelen kaçmaz | `transactions`'ta monoton anahtar yok | API-2, K-13 |
| Webhook + imza + tekrar deneme | DB outbox yok; dış HTTP yalnız VIES; dial-time SSRF koruması yok, egress açık | API-2, API-9, API-10, K-19 |
| Idempotency-Key, `limit ≤ 1000`, JSON hata gövdesi | Yok; `httpx.Limiter` ve 429'da `Retry-After` var | API-4 |
| OpenAPI + sandbox | Yok. Dokunuş simülatörü dört katlı kapının arkasında (`internal/config/config.go:731-745`). Panel yazma kapıları `Origin` yokken `Sec-Fetch-Site: same-site`'ı kabul ediyor (`internal/handler/adminlogin.go:1604-1614`, `signup.go:814-824`, `adminreset.go:1500-1510`, `activate.go:1601-1616`) | API-11, API-12, **K-3**, **GATE-1** |
| Bir telefon tek bir çalışana bağlı | Var — tarayıcı başına tek `tappa_session` çerezi | — |

**Eşleme notu.** KM-ERP departmanları (`DEP-1`…) spec'te **lokasyon**dur ve etiket onlara
bağlanır; KM'nin her departmanı KM tenant'ında bir lokasyon olur.

---

## 2. Sözleşme farkları — entegratöre bildirilecek

API-12'nin rehberinde bu liste **açıkça** yazılır.

1. Aktivasyon dokunuşu punch **değildir**; ilk punch ikinci dokunuştur (K-2). Aktivasyon yalnız **kendi şirketinin** bir plaketinde tamamlanır (K-1b).
2. `flag` verdict'li dokunuş yalnız **işveren şirketin yöneticisi onaylarsa**, onay anında akışa girer; reddedilen hiç gelmez (K-4, K-1a).
3. v1'de `version` hep `1`, `voided` üretilmez (K-5).
4. Ek alan: `channel` (`nfc` | `qr`); her iki kanalda `tagId` dolu.
5. `punchTime` = telefonun beyan ettiği an; çevrimdışı kuyruk **varsayılan** 120 sn'ye kadar `ok` (tenant'ça ayarlanabilir — `internal/policy/baseline.go:109-118`); tavan 72 sa. `recordedAt` = sunucunun yazdığı an.
6. 60 sn içindeki tekrar dokunuş punch değildir — kişi bazlı, **iki şirketin plaketleri arasında da**.
7. Lokasyon `timezone`'u tenant'ınkine eşit olmalı, değilse 422 (K-7).
8. Lokasyonda `active:false` yalnız etikettir (K-20).
9. Yeniden işe alım yeni `employee.id` üretir; `externalRef` aynı kalır (ADR 0010).
10. API ile açılan lokasyon panelden GPS/IP tanımlanana kadar dokunuşları onaya düşer.
11. E-posta ASCII olmalı, tenant'ın bir yönetici adresi olamaz (422); tenant içinde aktif başka çalışanda kullanılıyorsa 409 `email_unavailable`. Davet her zaman kuyruğa girer (K-17).
12. `resend-activation` canlı ya da son bir saatte gönderilmiş davet varken etkisiz (202).
13. Aktive olmuş çalışana `resend-activation` → 409 `already_activated`.
14. Aktive çalışanın e-postası değişirse davet gitmez (K-15).
15. Aktivasyon yalnız NFC ile (QR aktive edemez).
16. Webhook'ta sıra garantisi yok; tekilleştirme `id` + `version` ile.
17. Manuel kayıtlar akışa girmez (K-6).
18. Geçmiş sorgusu (`from`/`to`) en çok 31 gün, sayfalı.
19. `updatedSince` (rehber) 10 dakikalık örtüşme penceresiyle; tekrar dönenler `id` ile ezilir.
20. Sunucu yeniden başladığında (her deploy dahil) bekleyen webhook tekrar denemeleri ve kuyruktaki davetler, tenant'ın bir sonraki dokunuşuna ya da API çağrısına kadar bekler (K-19). İmleçli akış etkilenmez.
21. Henüz bağlanmamış çalışanın punch'ı `employee.externalRef: null` ile gelir.
22. Pasifleştirme anahtar başına saatlik bütçeyle sınırlı; aşılınca 429 (K-25).
23. API ve webhook, operatörün tenant için açtığı erişimle çalışır (K-22).
24. Ev lokasyonu olmayan çalışanın punch'ına "farklı şube" notu düşmez (K-14).
25. Şirketler arası dokunuşun kararı **işverenin** kurallarıyla verilir (K-1a); bir KF şubesine özel politika istisnası o şubede okutan KM çalışanına uygulanmaz. **İstisna:** şirketler arası QR okutması yalnız **o şubenin ağından** (IP) geliyorsa geçerlidir, değilse onaya düşer — işveren bunu kapatamaz (K-1g).
26. Holding üyeliği bittiğinde (operatör çıkarır) çapraz dokunuş durur: NFC'de, sayacı üyelik bitmeden ilerletilmiş dokunuş yine yazılır; bitişten sonraki NFC dokunuşu kayıtsız reddedilir (sayaç ilerlemez); QR ve manuel kayıt bitiş anından itibaren reddedilir (K-29).
27. **Şirketler arası punch'ta karşı tarafın kimlikleri** gelir: çalışanın `id`/`externalRef`/`employeeCode`/`company`'si ve lokasyonun `id`/`externalRef`/`name`/`company`'si, `tagId` (K-1f). Punch alanları **punch anındaki** değerlerdir (sonradan ad değişse de punch değişmez — K-32).
28. Başka şirketin kayıp/emekli bir plaketine dokunuş reddedilir; punch olmaz (K-4).
29. Rehber (`GET /v1/directory`) **sayfalıdır** (`cursor`, en çok 1000) — spec düz dizi gösteriyor.
30. KF şubesinde başlayıp **KM plaketinde** biten bir KM vardiyasında KF-RMS (`scope=location`) yalnız **girişi** alır; çıkış bir KM lokasyonu punch'ıdır ve KM-ERP'nin iki kapsamında gelir. KF panelinde holding çalışanlarının **dokunuşları** listelenir, **süreleri hesaplanmaz** ("süre işverende") — K-1e kaldırıldı.
31. **`scope=location` ile çalışma süresi hesaplayan entegratör dikkat:** başka şirketin çalışanının şube punch'ları, aradaki işveren olaylarını (diğer şirketin plaketlerindeki dokunuşlar) içermez; giriş/çıkışı yalnız bu kapsamdan eşlemek süreyi **fazla** sayabilir. Holding çalışanlarının süresi için işveren şirketin verisi (onun `employer` kapsamı) esastır.

---

## 3. Kararlar

### Kullanıcı kararları

| # | Karar | Seçilen | Bedeli (kayıtlı) |
|---|---|---|---|
| **K-1** | Şirketler arası dokunuş (CLAUDE.md §4.5) | **KF ve KM aynı holding altında ayrı iki tenant; holding bağıyla çapraz dokunuş** (2026-10-10) | CLAUDE.md §4.5'e ikinci, adı konmuş istisna (yalnız holding içi); §4.1'deki kapalı fonksiyon listesi. **Kazanç:** anahtar = tenant, EM-12 ön koşul değil, KM plaketleri yerinde, markalar ayrı |
| **K-1a** | Çapraz dokunuşun kaydı kimin? | **İşverenin (KM)**: KM politikası karar verir, KM yöneticisi onaylar, manuel düzeltme KM'de; KF salt okunur | KF'nin şube istisnaları KM çalışanına uygulanmaz (§2 madde 25); KM yöneticisinin KF sahası için yazdığı **manuel kayıtlar** KF'nin izdüşümüne `employer_manual` olarak düşer ve KF bunlara itiraz edemez → KF işgücü raporu bunları **toplamdan hariç**, ayrı gösterir (DPA notuna) |
| **K-1b** | Aktivasyon hangi plakette? | **Yalnız kendi tenant'ının** (ADR 0026 değişmez) | Yalnız KF'de çalışan KM çalışanı da ilk kez bir KM plaketine dokunmalı |
| **K-1c** | KM çalışanı KF plaketinde — tap/sonuç ekranı | **KF markasıyla** (logo + accent; sonuç ekranında logo) | CLAUDE.md §9 / ADR 0023 / ADR 0024 notu; logo baytı `holding_logo` ile (§4.1) |
| **K-1d** | Holding içi görünürlük | **Kabul, DPA notuyla**: ad, kod, zaman, mekan; GPS, IP, not, policy bağlamı, e-posta **asla** | DPA / aydınlatma metni (saklama süresi dahil) kullanıcıda |
| **K-1e** | KF'de başlayıp KM plaketinde biten vardiyanın kapanış çıkışı KF'ye gitsin mi? | ~~Evet, yalnız zaman~~ → **KALDIRILDI** (2026-10-10, kullanıcı): KF yalnız kendi plaketlerindeki dokunuşları görür | 7.–9. turun ORTA'larının çoğunu üretti (kapanış bağı sütunu, ADR 0011 çatışması, mükerrer/reddedilmiş kapanış, her tenant'ta kayıt kaybı); kaldırılınca `closes_transaction_id`, `closes_shift` ve manuel yol değişikliği plandan çıktı, görünürlük daraldı |
| **K-1f** | Çapraz punch'ta karşı tarafın kimlikleri | **Paylaşılır**: çalışan `externalRef`/kod, lokasyon `externalRef`/ad, `tagId`, şirket kodu | K-1d'nin genişlemesi; e-posta/GPS/IP/not yine asla |
| **K-1h** | Holding içi görünürlüğün genişlemesi | **Kabul** (2026-10-10): KF kendi plaketindeki KM dokunuşunun **sonucunu** (sayıldı / onay bekliyor / onaylandı / reddedildi — nedeni değil) ve **yönünü** (giriş/çıkış) görür; rehber **aktif/pasif** durumunu verir (spec şartı) | KF, KM'de işten ayrılanı dolaylı öğrenir (tersi de); DPA notuna eklenir |
| **K-1g** | Çapraz QR okutması | **Sistem kuralı: o şubenin IP'si şart**; yoksa `flag`. İşveren kapatamaz | Yeni `sys:` guardrail → CLAUDE.md §5 tablosu + ADR 0004/0007 notu |
| **K-2** | Aktivasyon dokunuşu punch mı? | **ADR 0026 kalır** | §2 madde 1 |
| **K-3** | Sandbox | **Ayrı alan adı** (`taptime.mt` dışı) + **ayrı derleme** (2026-10-10; alt alan + çerez geçişi denendi, maliyeti büyüdü, geri dönüldü). Panel yazma kapılarının `same-site`/`cross-site`'ı her dalda reddetmesi (**GATE-1**, K-37) yine yapılır | Alan adı + DNS |
| **K-19** | Arka plan işçisi tenant'lar arası tarama yapsın mı? | **Etkinlikle kurulan işçi — tarama YOK** | §2 madde 20 |
| **K-25** | API pasifleştirme ADR 0010'un iki adımlı onayını atlar | **Anında + anahtar başına saatlik bütçe** | §4.9 |
| **K-26** | Askıda savunma yazmaları | **Açık**: anahtar iptali, uç nokta devre dışı | §4.7 |

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe |
|---|---|---|
| K-4 | Akışa giren punch = `ok` ∧ kanal ∈ {`nfc`,`qr`} ∧ ¬`practice`, veya aynı şartı sağlayan **onaylanmış** `flag`. Başka hiçbir şey | Spec §3.5; CLAUDE.md §4.6 içeride karşılanır |
| K-5 | v1'de iptal yok → `version=1`, `status='valid'` | ADR 0011, 0009 |
| K-6 | Manuel ve practice akışa girmez | Spec §9 |
| K-7 | Lokasyon başına saat dilimi yok; farklıysa 422. Holding üyeleri **aynı saat dilimi** (K-36) | Raporlar tenant saat dilimiyle (CLAUDE.md §6) |
| K-8 | Dış kimlikler: `emp_`/`loc_`/`pch_`/`dlv_` + UUID; `tag_` + uid | |
| K-9 | Ayrı host `api.taptime.mt`, iki yönlü host kapısı | |
| K-10 | Anahtar `tt_live_` / `tt_test_` + 43 base64url; HMAC-SHA256, kendi env anahtarı; yanlış önek DB'ye gitmeden 401 | |
| K-11 | İmza spec biçiminde (`sha256=` + hex HMAC); ek zaman damgası başlığı yok | |
| K-12 | Uç nokta adresi/imza anahtarı panelden; anahtar bir kez gösterilir | |
| K-13 | İmleç = **hedef tenant başına sayaç satırı**; tek-hedefte 3. turda ölçüldü (0 kaçan, iki mutasyon kırmızı). İki hedefte kilit sırası K-31 | |
| K-14 | `employees.location_id` API satırlarında NULL olabilir; okuyucular API-1'de sayılır | Spec'te ev lokasyonu yok |
| K-15 | E-posta değişince bekleyen davetler aynı işlemde ölür; aktive değilse yeni davet kuyruğa | |
| K-16 | API'den yeni çalışan = otomatik davet | |
| K-17 | Davet kuyruğu `invite_requests`; kod gönderim anında basılır; kira 60 sn; resend istek anında öldürmez; **bugünkü tenant tavanları** | Ayrı tenant'lar → KM, KF'nin tavanını tüketemez |
| K-18 | İlk bağlama aynı tenant'ta e-posta eşleşmesiyle; lokasyonlarda panel "External ref" | |
| K-20 | Lokasyon `active` yalnız etiket | |
| K-21 | `employees.updated_at` tetikleyiciyle, yalnız rehbere görünen sütunlar | |
| K-22 | API ve webhook **yalnız operatör açınca**: `tenants.api_enabled_at` **ve `tenants.api_code`** (spec'in `company` alanı, ör. `KF`) — açan/kapatan ve kodu yazan tek yazar bir `op_*`; kapı her yolda | `/signup` herkese açık; `company` alanının kaynağı |
| K-23 | API audit: `actor_id = api_keys.id`, `detail.via = "api"` | |
| K-24 | E-posta tekilliği tenant içinde, pasif hariç, kısıt adı korunur; holding genelinde **aranmaz** (artık risk adlandırılmış: aynı kişiyi iki tenant davet edebilir; aktivasyon K-1b gereği o tenant'ın plaketinde fiziksel dokunuş ister) | Holding genelinde aramak yeni bir tenant-ötesi okuma olurdu |
| K-28 | `IsHardened()` = `prod` ∨ `sandbox`; dokuz `IsProd()` çağrısı (`config.go:326`, `config.go:1000`, `db/pool.go:119`, `session/cookie.go:136`, `adminauth/cookie.go:109`, `handler/cookies.go:270`, `signupstate.go:440`, `logincontext.go:297`, `activate.go:184`) | |
| K-29 | Holding üyeliği tenant kapsamsız `holdings` + `holding_members`; yalnız `op_*` yazar (`clock_timestamp()` — ADR 0021 §2 vii). **Kanal × zaman modeli:** `holding_advance_tag_counter` **üç sonuç** döner — `advanced` · `not_peer` · `replay` (eş ama sayaç artmadı); ilerletme **katı** (`left_at > clock_timestamp()`). **`SameHolding`'in tek kaynağı:** NFC'de ilerletme **denendiyse** dört sonucundan (`advanced`/`replay`/`peer_inactive` → eş, `not_peer` → değil; ilerletme plaketin **o anki** durumunu da döner ve `advance` onu `Decide`'a verir — bayat `tagRow.Status` değil, `checkin.go:730`); ilerletme **denenmediyse** (plaket aktif değil ya da CMAC geçersiz — `checkin.go:928-930`) ve QR'da `gather`'daki **katı** `holding_tap_site`'tan. `not_peer` → #1 → kayıt yok, sayaç ilerlemedi; `replay` → `sys:sun-invalid` reddi `site_*` ile; `advanced` olup `gather`'ın katı okuması 0 satır dönerse (`left_at` arada geçti) karar **saha kanıtı olmadan** verilir (→ `flag`) ve kayıt yazılır. Holding ilerletmesi `status='active'` plaketle süzülür (`checkin.go:919-921` değişmezi); **dördüncü sonuç** `peer_inactive` (eş ama plaket o anda aktif değil) → kayıtlı #2 reddi `site_*` ile (403'e düşmez). **Tolerans veritabanında zorlanır:** BEFORE INSERT ve holding tetikleyicisinin `transactions` kolu 60 sn toleransı **yalnız** iki NFC koluna uygular: (i) `sun_valid IS TRUE ∧ saha plaketinin last_ctr ≥ NEW.ctr` (gerçekten ilerletilmiş dokunuş; `sun_valid` kodda ilerletmenin başarısıdır — `checkin.go:966-970`); (ii) `verdict='reject'` — `ctr` şartı **olmadan** (red satırı mesai doğuramaz: yön NULL'a zorlanır ve yalnız `flag` onaylanır; replay, `peer_inactive` ve kayıp plaket reddi karar ile yazma arasında `left_at` geçse de kaybolmaz — 10. tur ölçtü: `ctr` şartıyla inaktif red düşüyordu). Karşılaştırmalar her iki tetikleyicide **`statement_timestamp()`** ile — BEFORE kabul edip AFTER "eşlik bitti" diyemez (10. tur ölçtü: FK beklemesinde `left_at` geçince KF tarafı sessizce atlanıyordu); kabul kapısı BEFORE'dur, AFTER'ın `transactions` kolu aynı toleransı kullanır. 8. tur ölçtü: ilerletilmiş 300 kabul, ilerletilmemiş 5000 ret. **QR, manuel ve onaylar:** katı (onaylarda da `statement_timestamp()`). **Adlandırılmış yarış:** QR'da katı karar ile INSERT arasında `left_at` geçerse INSERT reddedilir → "tekrar dene", tekrarda karar `not_peer` → kayıt yok (QR'da sayaç yok, harcanan bir şey yok). **Okumalar** (`holding_tap_site`, `holding_logo`, rehber, lokasyon listesi) **katı**. Tolerans ≥ 2 × istek zaman aşımı (30 sn — `internal/httpx/router.go:32,90`) testle pinlenir | 6. tur: "5 dk pencere" yalnız erteliyordu; 7. tur ölçtü: tek toleranslı pencere NFC'de sahte "replay", QR'da çıkarılmış tenant'a 60 sn yeni yazma, okumada `static_ips` açıyordu |
| K-30 | Çapraz dokunuşun kaydı **işverende**; `site_*` sütunları; `transactions` RLS'i değişmez. **CHECK kanala göre:** tap kanalları → `site_tenant_id` ve `site_tag_uid` dolu, `site_location_id` NULL olabilir (lokasyonsuz/`unassigned` plaket); `manual` → `site_tenant_id` ve `site_location_id` dolu, `site_tag_uid` NULL; her durumda `site_tenant_id ≠ tenant_id`, `location_id` ve `tag_uid` NULL; ve **`site_tenant_id IS NOT NULL OR (site_location_id IS NULL AND site_tag_uid IS NULL)`** (7. tur ölçtü: FK'ler MATCH SIMPLE olduğu için `site_tenant_id` NULL bir satır C'nin `site_*`'ını taşıyabiliyordu) | 6. tur: "üçü dolu" CHECK, lokasyonsuz KF plaketinin reddini ve KF sahası için manuel düzeltmeyi yazılamaz kılıyordu (§4.6) |
| K-31 | **Tek holding tetikleyicisi önce çalışır** (ad sırasında ilk): önce iki sayaç satırını kanonik sırada **var eder** (`INSERT … ON CONFLICT DO NOTHING`, küçük tenant id önce), sonra **tek ifadede** `… WHERE tenant_id IN (işveren, site) ORDER BY tenant_id FOR UPDATE` ile kilitler ve **kilitlediği satır sayısının 2 olduğunu** assert eder; holding punch'ının **iki hedef olay satırını da** bu tetikleyici yazar. Tetikleyici ad sırası katalog testinde pinlenir | 6. turda iki mercek ölçtü: iki ayrı tetikleyici → kilitlenme. 7. tur ölçtü: tek ön-kilit 14 288 işlemde 0 kilitlenme; ama sayaç satırı **eksikken** `FOR UPDATE` onu atlayıp kilitlenmeyi geri getiriyordu — "var et" adımı bunu kapatır |
| K-32 | `punch_events` olay anında bir **anlık görüntü** taşır (çalışan **id**/ref/kod/şirket kodu; lokasyon id/ref/ad/şirket kodu; tag uid); API okurken tenant sınırını aşmaz | KM'nin akışı KF lokasyon ref'ini, KF'ninki KM çalışan ref'ini istiyor; okuma anında çapraz join yerine yazma anında değişmez kopya |
| K-33 | **Holding süzgeci (normatif):** her `holding_*` fonksiyonu için "**anahtar satırının tenant'ı ∈ eşler(GUC tenant'ı)** ∧ GUC tenant'ı bir holding'in üyesi"; iki yönlü test: (i) holding dışı C çağırır → 0 satır; (ii) **eş çağıran (KM) + holding dışı anahtar** (C'nin uid'i/lokasyon id'si) → 0 satır ve C'nin `last_ctr`'ı değişmez. **Fikstürde C ikinci bir holding'in (H2) üyesidir** ve üç mutant kırmızı olmalı: M1 (yalnız çağıran üye mi), M2 (yalnız anahtar üye mi), M3 (ikisi de *bir* holding'in üyesi mi — `holding_id` eşleşmesi unutulmuş). **Tek yüklem:** bütün `holding_*` fonksiyonları **ve iki tetikleyici** (BEFORE INSERT üyelik kontrolü, holding AFTER tetikleyicisi) aynı `holding_peer(a, b, mod)` yüklemini çağırır (katalogda pinli; satır içi eşlik kontrolü yasak); mutantlar tetikleyici yoluna da uygulanır | 6. tur: yalnız (i) test edilirse "çağıranın üyeliği var mı" diye yazılmış hatalı süzgeç geçerdi ve KM oturumu C'nin plaketini okuyup sayacını harcayabilirdi; 7. tur ölçtü: C hiçbir holding'de değilken M3 iki testi de geçiyordu; 8. tur ölçtü: tetikleyicideki M3 biçimli kontrol, KM'nin manuel kaydına C'nin lokasyonunu yazdırıp C'nin `site_punches`'ına satır düşürüyordu |
| K-34 | `tappa_holdingdefiner` (BYPASSRLS) için **fiil × sütun matrisi** ve **"asla SELECT"** listesi: `tags.aes_key_ref`, `tags.app_key_ref`, `employees.email`, `admin_users.email`, `platform_admins.email`, `platform_admins.totp_secret_sealed`, `webhook_endpoints.url`, `webhook_endpoints.signing_key_sealed`, `transactions.gps_lat/gps_lng/source_ip/note/policy_context`, bütün `*_hash` sütunları; katalog `has_column_privilege(…,'SELECT') = false` ile pinler (7. turda prototipte 18 sütun ölçüldü). **Tetikleyici yolunda** gövde `NEW`'i grant'tan bağımsız okur → oradaki asıl bariyer holding tetikleyicisinin **yazdığı her tablonun** (`site_punches`, `punch_events`, `webhook_deliveries`, `feed_counters`) sütun katalog pinidir; serbest metinli sütun yok | BYPASSRLS rolde fonksiyon yolunun tek yapısal sınırı sütun grant'larıdır |
| K-35 | Canlı oturumlu ve bekleyen aktivasyon bağı olan tarayıcı, plaketin tenant'ı **davetin tenant'ından farklı** ve oturumun eşiyse normal holding dokunuşu olur; plaketin tenant'ı davetinkiyle aynıysa aktivasyon yine tamamlanır | `internal/handler/tap.go:369-385` aksi hâlde onu yabancı plaket aktivasyon hatasına çevirirdi |
| K-36 | Holding üyesi tenant'ın saat dilimi değiştirilemez — `tenants` üzerinde `tappa_holdingdefiner` sahipli bir tetikleyici (kapalı listede); değişiklik yalnız `op_*` ile, iki üyede birlikte | 6. tur: owner sonradan değiştirirse çapraz geç kalma hesabı bozulurdu |
| K-37 | **GATE-1:** panelin dört yazma kapısı (`sameOrigin` kopyaları) **her dalda** `Sec-Fetch-Site: same-site` ve `cross-site`'ı reddeder; **diğer her durumda bugünkü davranış aynen** (katı üç kopya ve Continue `Origin` yokken başlıksız/`none` isteği reddetmeye devam eder — login CSRF kapısı, `adminlogin.go:1585-1592`; `Submit` kabul eder). `no-referrer` sayfaları `Origin: null` + `same-origin` gönderir (ölçülmüş — `internal/httpx/operatorhost.go:53-57`) ve kabul edilmeye devam eder. Aktivasyon `Submit`'inin `strict=false` dalı (`activate.go:578,1603-1605`) `Origin` yokken de `Sec-Fetch-Site`'a bakar | Ucuz sertleştirme; K-3'ün geri dönüşüyle sandbox riski kalksa da `taptime.mt` altındaki her alt alan (ops dahil) için doğru kural |
| K-38 | **K-1g guardrail'ının yeri:** `sys:person-debounce`'tan (#8) **hemen sonra**, baseline'dan önce; guardrail sıra testine eklenir. Guardrail terminaldir: işveren onu gevşetemez ve `deny`'e de sıkılaştıramaz (adlandırılmış) | Debounce QR'ın tek frenidir (CLAUDE.md §5); önüne geçerse tekrar QR'lar onay kuyruğunu doldururdu |
| K-39 | **K-1d'nin ters yönü:** `holding_tap_site`'ın `static_ips`/GPS'i yalnız sunucu içi karar girdisidir, KM'ye gösterilmez. KM satırının `ip_match`/`policy_context`'i KF'nin ağını ve koordinatını dolaylı açabilir → KM panelinde `site_*` satırları için IP/GPS ayrıntısı gösterilmez (bugün de seçilmiyor — `db/queries/transactions.sql:606-615`); kalan dolaylı açılma K-1d bedeli olarak adlandırılır | 7. tur |

---

## 4. Tasarım özü (ADR 0027 ve 0028'de normatif olacak)

### 4.0 Ortak şema kuralı — `arwd` varsayılanına güvenilmez

Üretimde `pg_default_acl` = `tappa_app=arwd/tappa_owner`; taze CI'da `ar`. M11'in **her** yeni
tablosunda `REVOKE ALL … FROM tappa_app` + yalnız gereken fiil ve **sütunlara** açık `GRANT`;
UPDATE hep sütun listesiyle. **Matris** ADR 0027/0028'de; en az: `api_keys` UPDATE yalnız
`(revoked_at, last_used_at)`, iptal tek yönlü (00011 emsali) ve her `revoked_at` yazması
`COALESCE`/`IS NULL` korumalı (00011 BOUNDARY 2); `webhook_endpoints` UPDATE `tenant_id` içermez;
`feed_counters` `tappa_app` için yalnız SELECT. **Fonksiyonlar:** M11'in her fonksiyonunda
`REVOKE ALL ON FUNCTION … FROM PUBLIC`; **çağrılabilir** `holding_*` fonksiyonlarında yalnız
`tappa_app`'e EXECUTE — **istisna `holding_peer`**: SECURITY INVOKER, sahibi `tappa_holdingdefiner`,
EXECUTE yalnız sahipte (katalogda pinli — `prosecdef` sahip testi INVOKER'ı kapsamadığı için ayrı satır); **tetikleyici** fonksiyonlarında EXECUTE **yalnız sahipte** ve gövde
`TG_RELID`'i assert eder (4. turda geçici tablo yolu ölçüldü; 6. turda BYPASSRLS sahipte
`tappa_app`'e EXECUTE verilince öksüz satır yazılabildiği ölçüldü). **Tenant kapsamsız
tablolar** (`holdings`, `holding_members`): R5 muafiyetiyle görünür, `tappa_app`'ten açık
`REVOKE ALL`, ENABLE + FORCE RLS, `tenant_id` adlı sütun yok (CLAUDE.md §6). **Kabul:** CI
migration'lardan önce `arwd` varsayılanını kurar; genel katalog testi tablo fiillerini, sütun
yetkilerini ve fonksiyon EXECUTE ACL'lerini matrisle karşılaştırır.

### 4.1 Holding bağı ve çapraz dokunuş

**Varlık.** `holdings(id, name, created_at)`; `holding_members(holding_id, member_tenant_id,
joined_at, left_at)` — tenant kapsamsız (§4.0). Aynı anda tek aktif üyelik (`op_*` eski
üyeliğin geçişi bitmeden yenisini reddeder; çıkan tenant sonra yeniden katılabilir). `left_at`
geri alınamaz. Yazarlar yalnız `op_create_holding`, `op_add_holding_member`,
`op_remove_holding_member` (ADR 0021 sınıfı, `clock_timestamp()`); ekleme farklı saat
dilimini reddeder (K-36). Audit: `operator_audit_log` **ve** iki tenant'ın `audit_log`'u.

**Rol.** NOLOGIN, **BYPASSRLS**, üyesiz `tappa_holdingdefiner` (`01-roles.sql` idempotent blok +
canlı küme runbook'u; geri yükleme provası sıfırdan kurulmuş pod'a). Fiil × sütun matrisi ve
"asla SELECT" listesi **K-34**. Fonksiyonları: sabit `search_path = pg_catalog, pg_temp`,
nitelikli adlar, dinamik SQL yok, sabit dönüş sütunları, LIMIT; çağıranın tenant'ı **GUC'tan**,
girdi yalnız sunucunun çözdüğü anahtar; süzgeç **K-33**; eş değilse **0 satır** (bilinmeyen
anahtarla aynı cevap). Gövdede RLS yok → açık süzgeç tek bariyer (ADR 0021 §3.2).

**Kapalı fonksiyon listesi — holding istisnasının tamamı:**
| Fonksiyon | Ne döner / ne yapar | Çağrı yeri |
|---|---|---|
| `holding_tap_site(p_uid)` | eş tenant'ın plaketi: `site_tenant_id`, tag durumu, varsa `location_id`/ad/`external_ref`/`static_ips`/GPS/vardiya/overnight, saat dilimi, `api_code`, marka meta (accent; logo özeti yalnız VIES-doğrulanmış tenant için) | `GET /t`, `checkin.gather` (kilitli işlem içinde — `SameHolding`'in kaynağı) |
| `holding_advance_tag_counter(p_uid, p_ctr)` | **tek ifade** (`db/queries/tags.sql:33-46` CTE + `FOR UPDATE` kalıbı), eş şartı `prev`'de **ve** `UPDATE … WHERE`'de, **katı** üyelik (`left_at > clock_timestamp()`); sonuç `advanced` · `not_peer` · `replay` · `peer_inactive` (K-29) — **üyelik tek CTE'de bir kez** hesaplanır (`clock_timestamp()` iki kez değerlendirilmez): eş değilse `not_peer`; eş ama plaket aktif değilse `peer_inactive`; eş, aktif ama güncelleme boşsa `replay`; güncelleme doluysa `advanced`. 6.–8. turda ölçüldü: her turda tam 1 `advanced` | **`checkin.advance`** (`internal/domain/checkin/checkin.go:927-974`) — `sun.Verify`'a dokunulmaz (o yalnız aktivasyon) |
| `holding_logo(p_uid, p_digest)` | eş tenant'ın **güncel** logo baytı, yalnız VIES-doğrulanmışsa ve özet `holding_tap_site`'ın verdiğiyle aynıysa | logo rotası (ADR 0024 notu) — bugün `GET /t/logo/{sha}` yalnız oturum tenant'ınınkini veriyor (`internal/handler/brandlogo.go:240-245`) |
| `holding_site_locations(p_cursor)` | eş tenant'ın lokasyonları: id, ad, `external_ref`, vardiya, overnight — sayfalı, en çok 1000 | KM panelinde manuel kayıt lokasyon seçimi; KM raporları (ad + KF vardiyası) |
| `holding_directory(p_updated_since, p_cursor)` | holding'deki tenant'ların çalışanları: kimlik, `api_code`, `external_ref`, kod, ad, soyad, aktif — **e-posta yok**; sayfalı, en çok 1000 | `GET /v1/directory` |
| `holding_peer(a, b, mod)` | iki tenant'ın **aynı** holding'de eş olup olmadığı (K-33 tek yüklem); **SECURITY INVOKER**, EXECUTE **yalnız sahipte** — yalnız bu listedeki definer fonksiyonların ve tetikleyicilerin içinden çağrılır; `tappa_app`'e asla (aksi hâlde serbest parametreli bir üyelik kâhini olurdu — 9. tur ölçtü) | listedeki fonksiyonlar ve tetikleyiciler |
| Tetikleyiciler: `site_*` üyelik kontrolü (BEFORE INSERT, `WHEN (NEW.site_tenant_id IS NOT NULL)`; tolerans K-29'daki satır şartıyla, QR/manuel katı) · **holding akış/izdüşüm tetikleyicisi** (AFTER INSERT, ad sırasında ilk — K-31) · **saat dilimi kilidi** (BEFORE UPDATE ON `tenants` — K-36) | — | `transactions`, `transaction_reviews`, `tenants` |

**Kayıt (K-30).** `transactions`'a nullable `site_tenant_id`, `site_location_id`, `site_tag_uid`
(satır yeniden yazmaz — CLAUDE.md §4.3). FK `(site_location_id, site_tenant_id) → locations`,
`(site_tag_uid, site_tenant_id) → tags`; CHECK kanala göre (K-30); kısmi indeks. **ALTER
maliyeti:** 688 bin satırlık kopyada ~0,76 sn ölçüldü (6. tur); prod boyutunda ölçülür, **eşik
2 sn**, aşılırsa CHECK/FK `NOT VALID` + ayrı `VALIDATE`.

**Tap yolu.**
- `GET /t`: oturum KM, plaket KF → `holding_tap_site` 1 satır → sayfa **KF mekan adı + KF
  markası** (K-1c); 0 satır → bugünkü davranış. Canlı oturum + bekleyen aktivasyon + eş plaket
  → normal holding dokunuşu (K-35). Aktivasyon değişmez (K-1b; `activate.go:797,828`).
- `checkin.advance`: plaketin tenant'ı ≠ oturumunki → `holding_advance_tag_counter` (katı);
  sonucu karara taşınır: `not_peer` → bugünkü gibi ilerletmeden reddet (#1, kayıt yok);
  `replay` → `sys:sun-invalid` reddi `site_*` ile; `peer_inactive` → #2 reddi `site_*` ile;
  `advanced` → holding dokunuşu (K-29).
  **Eşzamanlılık:** aynı `(tag, ctr)` ile N goroutine, yarısı tek-tenant yarısı holding yolu →
  tam 1 başarı.
- `gather`: çalışan, departman vardiyası, son kayıt, debounce, son açık giriş **KM'de, bugünkü
  sorgularla** (6. tur kodda doğruladı: hepsi `(tenant, employee)` süzgeçli). Dokunulan
  lokasyonun kanıtı `holding_tap_site`'tan. `SameHolding` K-29'un tek kaynak kuralıyla:
  ilerletme denendiyse sonucundan, denenmediyse (plaket aktif değil / CMAC geçersiz) ve QR'da
  `gather`'daki katı `holding_tap_site`'tan; `advanced` + katı okuma 0 satır → saha kanıtsız `flag`.
- `tap.Decide`: guardrail #1 = `TagTenantID ≠ SessionTenantID ∧ ¬SameHolding`. **Yeni guardrail
  (K-1g):** `sys:cross-tenant-qr-site-ip` — kanal `qr` ∧ çapraz ∧ ¬(site IP eşleşti) → `flag`;
  yeri `sys:person-debounce`'tan hemen sonra, baseline'dan önce (K-38); işveren kapatamaz.
  ADR 0004/0007 + CLAUDE.md §5 tablosu.
- Politika kümesi **KM'nin** (K-1a). `write`: satır KM'de, `site_*` dolu.
- **Kayıp/emekli/lokasyonsuz KF plaketi:** KM'de `reject` kaydı (`site_tenant_id`, `site_tag_uid`
  dolu, `site_location_id` NULL olabilir — K-30); KF bunu izdüşümde `refused_tag_not_active`
  olarak görür ve **KF panelinin uyarı yüzeyi** (bugün `AlertLostTagTapped` yalnız oturum
  tenant'ının audit'ine yazıyor) bu izdüşüm satırından beslenir; çapraz audit yazılmaz.
- Sonuç ekranı: KF logosu (bugün farklı tenant'ta logo yok — `internal/handler/checkin.go:320-323`).

**KF görünürlüğü — izdüşüm `site_punches`.** KF tenant'ında RLS beşlisiyle, append-only:
`(tenant_id = site, id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr, sun_valid,
bucket_hour, occurred_at, channel, type, outcome, ctr_gap, created_at)`. **`outcome` kapalı küme** (K-1h):
`counted · pending_review · approved · rejected · refused_tag_not_active · refused ·
employer_manual` — `verdict` ve `matched_sid` **kopyalanmaz**. Eşleme tam ve **toplamdır**:
`ignored` (60 sn tekrarı) **izdüşüme yazılmaz** (açık bir dal, hata değil — bir `CASE`
hatası KM'nin kaydını geri alırdı, CLAUDE.md §4.6). GPS, IP, not, policy bağlamı, e-posta
**yok** (katalogla pinli). Satır türleri: (1) KF plaketindeki holding dokunuşu; (2) **işveren
manuel kaydı** (`employer_manual`) — KM yöneticisinin KF sahası için yazdığı manuel satır; KF
raporunda **ayrı ve toplamdan hariç** (fiziksel kanıtı yok, KF itiraz edemez — K-1a bedeli);
(3) onay olayı (`approved`/`rejected`).
**Red tekilleştirmesi** (9.–10. tur: satırdaki `ctr` saldırganın yazdığı değerdi; replay ile sahte
CMAC satırda ayırt edilemiyor — ikisi de `sys:sun-invalid` ve `sun_valid=false`): yalnız
**`sun_valid IS TRUE`** olan redler (gerçek, ilerletilmiş dokunuş — ör. deaktive çalışan) `ctr`
ile, `UNIQUE (tenant_id, site_tag_uid, ctr, employee_id) WHERE outcome LIKE 'refused%' AND
sun_valid`; **diğer bütün redler** (NFC'de `sun_valid` yanlış — replay dahil —, QR redleri)
`ctr`'sız, **sunucu saatinden** saat kovasıyla: `bucket_hour = date_trunc('hour', NEW.created_at
AT TIME ZONE 'UTC')` (istemcinin `occurred_at`'i değil; oturum saat diliminden bağımsız),
`UNIQUE (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) WHERE NOT sun_valid AND
outcome LIKE 'refused%'` — `outcome` anahtarda (sahte bir `refused` kayıp plaket uyarısını
bastıramaz); iki ekleme de `ON CONFLICT DO NOTHING` — KM'de her red yine yazılır (§4.6), KF'ye
(ve kayıp plaket uyarısına) en çok bir kez. (`site_punches`'ta `sun_valid` sütunu tutulur.)
**KF işgücü raporu holding çalışanları için süre hesaplamaz** (10. tur ölçtü: izdüşüm, işverenin
zincirindeki aradaki olayları görmediği için "en geç giriş → en erken çıkış" eşlemesi KF'de hiç
geçmemiş 9 sa / 32 sa aralıklar sayıyordu): holding çalışanlarının dokunuşları zaman, yön ve
**son** sonuçla (`transaction_id` başına son satır — onay satırı ana kaydın `occurred_at`/`type`'ını
taşır) listelenir, süre sütunu "işverende" yazar. KF'nin **kendi** çalışanlarının süreleri
değişmez. **Adlandırılmış bedel:** üyelik bittikten sonra verilen onay/ret KF'ye düşmez; o satır
KF'de "sonuç işverende" olarak kalır.
KF'nin bütün okumaları sıradan, RLS'li, açık `tenant_id` filtreli sorgulardır.

**KF ve KM panelleri.**
- KF: holding dokunuşları (liste; süre "işverende"), plaket son görülme ve **sayaç boşlukları**
  (`ctr_gap`) izdüşümden. KF onay vermez.
- **İşverenin okuyucuları** (rol geneldir: KF@KM'de saha KM'dir) — `transactions.location_id`/
  `tag_uid` okuyan **dokuz** çağıran (7. tur saydı): `ListPanelTransactions`,
  `ListFlaggedForReview`, `ListOpenCheckIns` (saha mekan adı), `ListWorkedShiftEvents` +
  `resolveShift` (saha vardiyası, "<şirket> — <mekan>" kovası; bugün `uuid.Nil` kovasına düşüyor —
  `report.go:838-841`; CSV dışa aktarım da `ledger.Hours` üzerinden bunu kullanır),
  `ListAnomalyVenues`, `ListTapsTakenTogether` (bugün dışarıda bırakıyor), `ListAnomalyPlaques`
  (`anomaly.go:450`; saha plaketi işverenin anomalisi değildir → dışarıda, saha bunu `ctr_gap`
  ile görür), `ListTagLastSeen` (`plaque.go:674`; aynı), `CountLocationReferences` (saha tarafı
  izdüşümle). Faturalama, kadro ve operatör tarafında okuyucu yok. Kaynak `holding_site_locations`.
- **Lokasyon silme:** KF'nin `CountLocationReferences`'ı KM'deki `site_location_id`'leri göremez →
  sayım izdüşümden de; FK RESTRICT son kemer.
- **Manuel kayıt** yalnız işverende; KF sahası için `site_tenant_id` + `site_location_id` (lokasyon
  seçimi `holding_site_locations`'tan), aynı tetikleyiciden geçer (katı eşlik); KF'de `employer_manual`.

**Değişen kurallar (API-0'da).** CLAUDE.md §4.5: *"Bilinçli istisnalar iki tanedir: (1) `op_*`
(ADR 0021); (2) aynı holding'deki tenant'lar arası dokunuş ve görünürlük — yalnız §4.1'deki kapalı
`holding_*` fonksiyon listesi ve holding tetikleyicileri; üyeliği yalnız `op_*` yazar (ADR 0027)."*
CLAUDE.md §5 (yeni guardrail K-1g; guardrail #1'in eşlik şartı), §6 ("tenant kapsamsız tablo
yalnız platform operatörü" cümlesine `holdings`), §9 (K-1c). ADR 0002 md.6 ve md.7, 0004, 0006
(debounce iki tenant'ın plaketlerini kapsar), 0007, 0008 (yön zinciri), 0016 §2 (korunur: KM
oturumundan hiç `WithTenant(KF)` açılmaz), 0021 (başlık, "TEK", §3.1, §6 testlerinin `holding_*`
karşılığı), 0023, 0024 (logo rotası), 0026 (değişmediği notu); `guardrails.go:308-309`'daki
"KF yöneticisi KM çalışanının adını asla görmez" yorumu; `open-questions.md` Y2.

### 4.2 API anahtarı

- `api_keys(tenant_id, id, name, token_hash, prefix_hint, created_by, created_at, last_used_at,
  revoked_at)` — RLS beşlisi; `token_hash` küresel UNIQUE.
- Kimlik: Bearer → biçim kontrolü → IP kovası → yedinci resolver `resolve_api_key_by_hash`
  ("taşı, uygulama": `(tenant_id, key_id, revoked_at, api_enabled_at)`; iptal 401, kapalı 403;
  önbellek yok; `tappa_resolver`'a yalnız `SELECT (id, api_enabled_at) ON tenants`).
- **Yetki:** yazma = anahtarın tenant'ının çalışan/lokasyonları; `scope=location` = hedef tenant
  anahtarınki ∧ rol `location`; `scope=employer` = hedef tenant anahtarınki ∧ rol `employer`;
  rehber holding geneli (`holding_directory`). Holding dışı tenant'ın anahtarı yalnız kendi
  tenant'ını görür.
- Panel (owner): oluştur (bir kez) · listele · iptal; audit.
- **Log ve tarama:** anahtar, hash, `Authorization`, uç nokta imza anahtarı, webhook URL'si ve
  `*url.Error` metni asla. R7'ye ağacı kırmızıya çevirmeyen kökler (`api_?key`, `bearer`,
  `signing_?key`) ve log çağrısında `Header.Get("Authorization")` dar kalıbı; R7b'ye yeni ad
  alanları (izdüşümün `employee_name`'i dahil); R7d'ye iki önekli değer kalıbı; **R4**'e şema
  nitelikli `UPDATE public.tags SET last_ctr` biçimi (bugün görmüyor — `scripts/redline-check.sh:345`).
  Her ekleme kasıtlı ihlalde kırmızı testiyle.
- **Env anahtarları** `namedKeys()`'e girer, bütün anahtarlarla çift çift karşılaştırılır; API-13
  runbook'u prod çiftlerini değer basmadan ölçer.

### 4.3 Punch olayı — yapısal yayın

- **Tablolar (API-2):** `punch_events(tenant_id, seq, punch_id, punch_tenant_id, roles, version,
  status, punch_time, recorded_at, channel, employee_id, employee_ref, employee_code, employee_company,
  location_id, location_ref, location_name, location_company, tag_uid, created_at)` — `tenant_id`
  = **hedef**; `roles ⊆ {employer, location}`; `UNIQUE(tenant_id, seq)`; FK
  `(punch_id, punch_tenant_id) → transactions(id, tenant_id)`; **anlık görüntü** sütunları (K-32);
  append-only; R3 kapsamı. `feed_counters(tenant_id, last_seq)`. `webhook_endpoints`,
  `webhook_deliveries`.
- **Hedef satırlar:** tek-tenant punch → bir satır `{employer, location}`; holding punch'ı →
  iki satır: işveren `{employer}`, plaketin tenant'ı `{location}`.
- **Tetikleyiciler (binary'den bağımsız; saf `tap.Decide`, `policy`, sayaç yolu dokunulmaz):**
  1. **Holding tetikleyicisi** (`tappa_holdingdefiner`, ad sırasında **ilk**). **Sıra:** önce
     eşlik kararı (`holding_peer`, `statement_timestamp()`) ve — olay yazılacaksa — kanonik
     sayaç kilidi (K-31), **sonra** her olay satırı (işveren olayı sayacı artırdığı için kilit
     ondan önce alınmazsa çift kilit ters sırada alınır — 6./7. turun kilitlenme sınıfı).
     `site_*` taşıyan ve **K-4 şartını sağlayan** her satırda (ve bu satırlara bağlı onaylarda)
     işverenin **olay ve teslim** satırını **eşlikten bağımsız** yazar (9. tur: 9. sürümde işverenin teslim
     satırı yazarsız kalmıştı — KM-ERP webhook'u KM@KF'yi hiç almazdı); eşlik sürüyorsa
     (`holding_peer`, K-29 kuralı; onaylarda katı) ayrıca iki sayaç satırını var edip kanonik
     sırada kilitler (K-31), saha olay ve teslim satırını yazar; **K-4'ten bağımsız olarak** da
     eşlik sürüyorsa `site_punches` satırını yazar (redler ve onay bekleyenler dahil — toplam
     eşleme); eşlik bitmişse işverenin satırını karşı taraf anlık görüntüsü NULL olarak yazar.
     İşverenin satırındaki karşı taraf (saha lokasyonu ref/ad/şirket kodu) ancak burada
     okunabilir (NOBYPASSRLS feedwriter GUC=KM iken KF satırlarını göremez). Yazdığı saha
     satırlarının `tenant_id`'si yalnız `NEW.site_tenant_id` olabilir.
  2. **Kendi-tenant tetikleyicisi** (`tappa_feedwriter`, NOBYPASSRLS, ad sırasında ikinci):
     `site_*` taşıyan satırlara ve onlara bağlı onaylara **hiç dokunmaz**; yalnız
     `site_tenant_id IS NULL` satırların olay ve teslim satırlarını yazar.
  - Onay (`transaction_reviews`) için aynı iki tetikleyici; bağlı satır K-4'ün kanal/practice
    şartını sağlıyorsa.
- **`tappa_feedwriter` yetkisi** 3.–5. turda ölçülmüştü; bu sürümde liste değişti (şirket
  sütunları çıktı, `site_tenant_id` ve anlık görüntü için gereken sütunlar girdi) → **API-2'de
  yeniden ölçülür** ve 25/26-kalem türü "her biri tek başına zorunlu" kanıtıyla pinlenir.
- `tappa_app`: `punch_events`, `feed_counters`, `site_punches` SELECT; `webhook_deliveries`
  SELECT + kira/sonuç sütunlarına UPDATE. INSERT/DELETE hiçbirinde yok.
- **Rollerin yaşam döngüsü ve katalog** (iki definer): `01-roles.sql` idempotent blok + runbook;
  migration ön koşulu rol yoksa ya da iki yönden üyelik varsa düşer (00029:99-120);
  `internal/db/pool.go`'nun rol reddi sertleştirilmiş ortamda `tappa_app` herhangi bir rolün
  üyesiyse açılışı reddeder; katalog: NOLOGIN, beklenen BYPASSRLS, üyesiz, `prorettype = trigger`
  olan fonksiyonlarda EXECUTE yalnız sahipte, **tetikleyici ad sırası**, `proconfig` sabit,
  K-34 sütun pinleri; ADR 0021'in `prosecdef` sahip testi iki rol ile genişler.
- **Değişmezler:** `SET search_path`, nitelikli adlar, açık tenant filtreleri, GUC assert'i
  (kendi-tenant kısmı), `TG_RELID` assert'i; hata yutan `EXCEPTION` yok (eşlik bitince atlama
  açık bir `IF`'tir, hata yutma değil); uç nokta adresini okumaz; ağ/uzun iş yok.
- **Geri doldurma:** `transactions` ve `transaction_reviews` kilitlenir, tetikleyiciler kurulur,
  geçmiş tek-tenant satırları için olay yazılır (bugün holding satırı yok); teslim satırı yok;
  eşik 2 sn.
- **Bedel (adlandırılmış):** tetikleyici hatası `ok` dokunuşun kaydını geri alır. **Yeni
  tenant'lar arası bağımlılık:** KF'de sayaç satırını tutan takılı bir işlem, KM'nin holding
  dokunuşlarını `lock_timeout` sonunda "tekrar dene"ye düşürür (sayaç ilerlemiş, kayıt yok —
  bugünkü sınıf); idle zaman aşımı bunu sınırlar.
- **Eşlik eden listeler:** `scriptguards_test.go:447-510` (`APPEND_ONLY` — `punch_events`,
  `site_punches`), `pg-restore-verify.sh` (`trunc_tables`, "seven" cümlesi).

### 4.4 Akış imleci — neden kaybolmaz

Her hedef satırdan önce hedef tenant'ın sayacı `INSERT … ON CONFLICT (tenant_id) DO UPDATE SET
last_seq = last_seq + 1 RETURNING last_seq` ile artar; satır kilidi commit'e kadar → aynı hedefte
`seq` commit sırasıyla artar, `seq > cursor` hiçbir satırı atlamaz, abort sayacı geri alır
(3. turda ölçüldü). **İki hedef:** K-31'in tek ön-kilidi; API-2 stresi ters yönlü holding
dokunuşlarıyla **0 kilitlenme, 0 kaçan**. Kilit değişmezi: sayaç satırları kilitlendikten sonra
çakışan kilit, ağ, uzun iş yok. Takılı yazıcı: `tappa_app` için `idle_in_transaction_session_timeout`
+ TCP keepalive (havuz `RuntimeParams`, açılışta `SHOW`); tetikleyicilerde öznitelik `lock_timeout`.
Sayaç satırı her tenant için her zaman var (migration + `AFTER INSERT ON tenants` tetikleyicisi);
yine de holding ön-kilidi satırları önce "var eder" ve kilitlediği sayıyı assert eder (K-31 — 7.
tur eksik satırla kilitlenmeyi ölçtü). Migration sırası: tetikleyiciler **geri doldurmadan önce**
kurulur. Geri yükleme: runbook her tenant'ın sayacını ileri atar; doğrulayıcı
`count(tenants) = count(feed_counters)` ve `last_seq ≥ max(seq) + aralık`'ı kendi verisinden
denetler; T45 tipi elle prova. Geçmiş sorgusu ≤ 31 gün, sayfalı.

### 4.5 Webhook

- `webhook_endpoints(tenant_id, id, url, signing_key_sealed, active, created_at, rotated_at)` —
  `UNIQUE(tenant_id)`; anahtar `sun.Seal(TAPPA_WEBHOOK_KEK, AAD = endpoint id)`, önekli; rehber
  vektörünün anahtarı açıkça sahte. URL: `https`, 443, IP literali, userinfo, sorgu, parça yok;
  URL ve `*url.Error` metni log/audit'e asla.
- `webhook_deliveries(…)` — `UNIQUE(endpoint_id, punch_event_seq)`; `last_error_class` kapalı küme.
- **Yönlendirme:** her hedef olay satırı kendi tenant'ının uç noktasına bir teslim → KF@KF 1 ·
  KM@KM 1 · KM@KF 2 · KF@KM 2.
- **Yük:** spec §5 + `channel` + anlık görüntü (K-32); IP, GPS, mesafe, not, policy bağlamı asla.
  Ping sentetik; ping ve "yeniden gönder" aynı kovada (tenant başına saatte 10).
- **İşçi:** kiralama (kısa işlem → HTTP işlem dışında → sonuç ayrı işlem); gönderimde ve yeniden
  gönderimde K-22 ve uç noktanın `active`'i; geri çekilme 1 dk · 5 dk · 30 dk · 2 sa · 6 sa → `dead`.
  Kurulum K-19 (holding punch'ında iki tenant da kümeye girer).
- **SSRF:** `net.Dialer.ControlContext` gerçekten bağlanılan adrese; `Unmap()`; IANA özel amaçlı
  kayıtları birebir; IPv4 küresel unicast; IPv6 yalnız `2000::/3`, `2001::/23`, `3fff::/20`,
  `2002::/16` düşülerek; düğüm/ingress adresleri değil (liste boşsa prod/sandbox başlamaz);
  `Proxy: nil`; yönlendirme yok; 10 sn; `MaxResponseHeaderBytes`; gövde okunmaz.
- **Kapanış bütçesi:** `srv.Shutdown` ile eşzamanlı, 20 sn içinde.

### 4.6 Ortak sözleşme

JSON hata gövdesi; `Retry-After`; `limit` ≤ 1000; gövde ≤ 64 KiB. Idempotency-Key satırı iş
işlemiyle aynı işlemde, gövdesiz, 24 sa tembel silme. `GET /employees` e-postasız. OpenAPI 3.1 elle,
embed, `GET /v1/openapi.yaml`.

### 4.7 Askıya alma (ADR 0025 + K-26)

| Askıda açık | Askıda 403 `tenant_suspended` |
|---|---|
| bütün `GET`'ler, akış, rehber, webhook gönderimi · yeni çalışan `PUT` · `resend-activation` · davet kuyruğu işçisi · tetikleyicilerin olay/teslim/izdüşüm yazması (askı dokunuşu durdurmaz — holding dokunuşu dahil) · **K-26:** anahtar iptali, uç nokta devre dışı | `PUT /locations` · var olan çalışanı değiştiren `PUT` (`active:false` dahil) · `DELETE /employees` · anahtar oluşturma · uç nokta oluşturma/döndürme/adres değiştirme · test ping · "yeniden gönder" |

Tabloda adı geçmeyen her M11 yazması askıda kapalı. Askı sütunları OP-15'e bağlı → API-4 kontrol
noktası, testleri API-4b (OP-15'e bağlı).

### 4.8 Sandbox (K-3) ve yazma kapıları (GATE-1)

- **Ayrı alan adı** (`taptime.mt` dışı) — `internal/handler/cookies.go:138-159` kısıtı hiç
  tetiklenmez, çerez geçişi gerekmez.
- **Ayrı derleme:** simülasyon ayrı pakette, yalnız `sandbox` tag'li `cmd` dosyası import eder;
  `go list -deps` prod için negatif, sandbox için pozitif; CI `-tags sandbox`; `verify-image.sh`
  tag'i doğrular; sandbox ikilisi yalnız `TAPPA_ENV=sandbox`, tag'siz ikili `sandbox`'ı reddeder.
- Sertleştirme K-28; sanal UID izin listesi (gerçek NXP `0x04` reddi) her çözümlemede/eklemede;
  kendi sırları; prod yedeği oraya geri yüklenmez; simülasyon holding dokunuşunu da üretir;
  API/webhook/holding yalnız sandbox operatörüyle; gerçek e-posta yok; NetworkPolicy, PSS
  `restricted`, kota.
- **GATE-1 (K-37):** dört `sameOrigin` kopyası her dalda `same-site`/`cross-site`'ı reddeder,
  diğer her durumda bugünkü gibi; Android rölesinin (`android/.../HttpRelayServer.kt:17-19`)
  "same-site" yorumu güncellenir (röle `Origin` dalında kalır).

### 4.9 Pasifleştirme bütçesi (K-25)

`DELETE` ve `PUT {active:false}` anahtar başına saatlik bütçe (varsayılan 20); audit satırlarından,
anahtar başına advisory kilit altında; kilit, sayım, pasifleştirme ve `RecordTx` audit tek işlemde.
Aşılınca 429 + owner uyarısı + audit. ADR 0010 sapma notu.

---

## 5. Görevler

| ID | Görev | Boyut | Ajan | Bağımlılık |
|---|---|---|---|---|
| API-0 | **ADR 0027** (holding: tablolar, `op_*`, `tappa_holdingdefiner` + K-34 matrisi, kapalı fonksiyon listesi + K-33 süzgeci, `site_*` + K-30, K-29 tolerans modeli, izdüşüm + kapalı `outcome` kümesi, K-1a…h (K-1e kaldırıldı), K-31, K-33 tek yüklemi, K-35, K-36, K-38, K-39) + **ADR 0028** (dış API, anlık görüntülü punch olayları, iki-hedefli imleç, tetikleyici değişmezleri ve ad sırası, webhook, SSRF tehdit modeli, K-19, sandbox, K-25) + **CLAUDE.md** §4.5 (ikinci istisna), §5 (K-1g guardrail, #1 eşlik), §6 (`holdings`), §7 (log yasakları), §9 (K-1c), §3 (yeni paketler) + notlar: ADR 0002 md.6/md.7, 0004, 0006, 0007, 0008, 0010, 0016, 0021, 0022 §7, 0023, 0024, 0025, 0026; `guardrails.go` yorumu; `open-questions.md` Y2 | L | yapıcı + üçüncü göz + güvenlik | — |
| GATE-1 | Panel yazma kapıları `same-site`/`cross-site`'ı her dalda reddeder (K-37) | S | yapıcı + güvenlik | API-0 |
| API-1 | Tek-tenant şema: `external_ref`, `employee_code`, `first_name`/`last_name`, `updated_at` + K-21, `invite_requests`, `locations.external_ref`/`active`, K-14, K-24, `tenants.api_enabled_at` + `api_code` + `op_*`; §4.0 katalog iskeleti; panel alanları | M | `tappa-db-migrator` + yapıcı (`tappa-brand`) | API-0 |
| API-1a | Holding varlığı: tablolar, üç `op_*`, `tappa_holdingdefiner` + K-34, K-36 saat dilimi kilidi, katalog, operatör ekranı | M | `tappa-db-migrator` + yapıcı | API-0 |
| API-1b | Çapraz dokunuş: `site_*` + K-30 CHECK + üyelik tetikleyicisi (ALTER ölçümü) · `holding_tap_site`, `holding_advance_tag_counter`, `holding_logo` · guardrail #1 + K-1g · `checkin.advance`/`gather`/`write`, `tap.go` (K-35), logo rotası · tap/sonuç ekranında KF markası | L | yapıcı (`tappa-sun`, `tappa-brand`) | API-1, API-1a |
| API-1c | `site_punches` + holding tetikleyicisinin **izdüşüm kısmı** (API-2 aynı tetikleyiciye sayaç ve olay ekler) + red tekilleştirmesi + K-1h + KF işgücü raporunun izdüşüm içi eşlemesi · KF panel görünümleri ve kayıp plaket uyarısı · işverenin dokuz okuyucusu + `holding_site_locations` · manuel saha kaydı (`employer_manual`, toplamdan hariç) · lokasyon silme sayımı | L | yapıcı (`tappa-brand`) | API-1, API-1b |
| API-2 | `punch_events` (anlık görüntü), `feed_counters` (+ `tenants` tetikleyicisi), `webhook_endpoints`, `webhook_deliveries` · iki tetikleyici (ad sırası, K-31) · `tappa_feedwriter` yeniden ölçüm · geri doldurma · zaman aşımları · geri yükleme · eşlik eden listeler | L | `tappa-db-migrator` | API-1, API-1c |
| API-3 | `api_keys` + resolver + `internal/apikey` + panel + audit + R4/R7/R7b/R7d + env ayrımı | M | yapıcı (`tappa-brand`) | API-1 |
| API-4 | `/v1` iskeleti (host kapısı, biçim + IP kovası + Bearer + anahtar kovası, JSON hata, gövde sınırı, Idempotency, askı kontrol noktası, `openapi.yaml`, K-19 işçisi) | M | yapıcı | API-3 |
| API-4b | Askı tablosunun testleri | S | yapıcı | API-4, **OP-15** |
| API-5 | `PUT /v1/locations/{ref}` | S | yapıcı | API-4 |
| API-6 | Çalışan uçları, davet kuyruğu işçisi, K-25 | L | yapıcı | API-4 |
| API-7 | `GET /v1/punches` | M | yapıcı | API-2, API-4 |
| API-8 | `GET /v1/directory` (`holding_directory`) | S | yapıcı | API-1a, API-4 |
| API-9 | Uç nokta paneli + SSRF-güvenli istemci | M | yapıcı (`tappa-brand`) | **GATE-1**, API-2, API-4 |
| API-10 | Webhook gönderici + teslim günlüğü | L | yapıcı (`tappa-brand`) | API-9 |
| API-11 | Sandbox ikilisi + simülasyon + kurulum | M | yapıcı + kullanıcı (alan adı/DNS) | API-5…API-10 |
| API-12 | OpenAPI + entegrasyon rehberi (§2'nin 30 maddesi + imza vektörü) | S | yapıcı | API-5…API-11 |
| API-13 | Canlıya alma: sırlar, `api.` DNS + Ingress, **`tappa-security-auditor` tam tur**, operatörden K-22 (kod `KF`/`KM`) ve **KF+KM holding'i**, K-18 runbook'u, prod anahtar çifti ve `pg_auth_members` ölçümü, spec §7 listesi | M | yapıcı + denetçi + kullanıcı | hepsi + **OP-15** |

Her görev: yapıcı (opus) → ayrı üçüncü göz → bulgu varsa düzelt + yeniden denetle; CLAUDE.md §4'e
değen görevlerde (API-0, GATE-1, API-1a, API-1b, API-1c, API-2, API-3) ayrıca
`tappa-security-auditor`. Büyük testler yalnız görev sonunda. Dal `m11-api`. Her commit'ten önce
`./scripts/redline-check.sh`.

### Kabul çekirdeği

- **API-0:** CLAUDE.md §4.5 (iki istisna, kapalı listeyle), §5 (K-1g + #1), §6, §7, §9 yeni metinleri; bütün ADR notları; ADR 0027'de holding tehdit modeli (holding dışı tenant hiçbir yoldan erişemez; eş tenant holding dışı anahtar veremez), ADR 0028'de SSRF tehdit modeli.
- **GATE-1:** dört kapının her dalı (aktivasyon `Submit`'inin `strict=false` dalı dahil) `Sec-Fetch-Site: same-site`/`cross-site`'ı — `Origin` olsa da olmasa da — reddeder; `Origin: null` + `same-origin` (no-referrer sayfaları) kabul; diğer her başlık bileşiminde sonuç **bugünkünün aynısı** (tablo testiyle pinlenir — katı kopyalarda `Origin` yokken başlıksız/`none` RED kalır); mevcut panel/kayıt/sıfırlama/aktivasyon/röle akışları yeşil.
- **API-1:** §4.0 katalogu (`arwd`); K-24 indeksi ve kısıt adı; pasif satırla aynı e-postada yeni aktif satır açılır, iki aktif açılamaz; `updated_at` dört UPDATE yolunda ve yalnız rehber sütunlarında; okuyucu sayımı; `api_enabled_at`/`api_code`'u `tappa_app` yazamaz.
- **API-1a:** `holdings`/`holding_members`'a `tappa_app` hiçbir fiille dokunamaz (`arwd`); yalnız `op_*` yazar, audit üç günlükte; farklı saat dilimi reddedilir; üye tenant'ın saat dilimi owner tarafından değiştirilemez (K-36); aynı anda iki aktif üyelik yok, çıkan tenant geçiş bitince yeniden katılabilir; `left_at` geri alınamaz; K-34 sütun pinleri — `aes_key_ref`, `app_key_ref`, `employees.email`, `admin_users.email`, `platform_admins.email`, `platform_admins.totp_secret_sealed`, GPS/IP/not/`policy_context`, `*_hash` → SELECT false; `holding_peer` tek yüklem ve katalogda pinli; geri yükleme sıfırdan pod'a.
- **API-1b:** (a) KM oturumu + KF plaketi → kayıt KM'de, `site_*` dolu; (b) **K-33 iki yön, C ikinci bir holding'in (H2) üyesiyken:** C oturumu + KF plaketi → 403, kayıt yok, KF sayacı ilerlemez; **KM oturumu + C plaketi** → 403, kayıt yok, **C'nin `last_ctr`'ı değişmez**; her `holding_*` fonksiyonu C bağlamında ve KM bağlamı + C anahtarıyla 0 satır; **M1, M2, M3 mutantlarının her biri kırmızı — fonksiyonlarda ve iki tetikleyicide**; **KM manuel kaydı + C lokasyonu → ret, C'nin `site_punches`'ı 0**; (c) aynı `(tag, ctr)` N goroutine yarı/yarı → tam 1; (d) KF sonra 60 sn içinde KM plaketi → `ignored`; KF'de giriş + KM'de çıkış → doğru `out`; (e) KF'nin `location/<id>` istisnası uygulanmaz; (f) **K-1g:** çapraz QR site IP'siz → `flag` (KM QR kuralını gevşetse bile), site IP'li → `ok`; (g) **K-29 kanal × zaman:** NFC — ilerletme `left_at`'ten önce, yazma sonra → holding kaydı yazılır; **`left_at` ilerletme ile `gather` arasında → katı okuma 0 → saha kanıtsız `flag`, hata değil**; ilerletme `left_at`'ten sonra → 403 (`not_peer`), kayıt yok, sayaç ilerlemez; eş + eşit ctr → `replay` → `sys:sun-invalid` reddi `site_*` ile; **kayıp/emekli KF plaketi (ilerletme denenmez) → katı `holding_tap_site` ile eş → #2 reddi KM'de, KF'de `refused_tag_not_active`**; **plaket ilerletme anında aktif değil → `peer_inactive` → #2 reddi `site_*` ile (403 değil)**; **replay, `peer_inactive` ve kayıp plaket reddi karar ile yazma arasında `left_at` geçse de yazılır**; **ilerletme anında plaket aktiften pasife geçerse `Decide` güncel durumu görür (#2, kanca ile kurulan yarış)**; **BEFORE kabul edip AFTER'ın KF tarafını atladığı satır yok (FK beklemesinde `left_at` geçen yarış — `statement_timestamp()`)**; **ilerletilmemiş bir NFC satırı (`last_ctr < ctr`, `sun_valid` yanlış, `ok`/`flag`) `left_at`'ten sonra INSERT'te reddedilir**; **`holding_peer`'in sahibi `tappa_holdingdefiner`, SECURITY INVOKER, EXECUTE yalnız sahipte (`tappa_app` çağıramaz; katalog satırı)**; QR — `left_at`'ten sonraki karar → 403, kayıt yok; manuel — `left_at`'ten sonra KF sahası reddedilir; `holding_tap_site`/`holding_logo` `left_at`'ten sonra 0 satır; tolerans ≥ 2 × istek zaman aşımı pinli; (h) K-1b aktivasyon reddi; K-35 bekleyen bağlı tarayıcıda holding dokunuşu; (i) tap/sonuç ekranında KF logosu ve accent'i, VIES kapısı, `holding_logo` C'nin ya da eski özetin baytını vermez; (j) CHECK kanal kuralları (lokasyonsuz plaket reddi yazılır; manuel KF sahası yazılır; `site_tenant_id = tenant_id` reddedilir; **`site_tenant_id` NULL iken `site_location_id`/`site_tag_uid` dolu satır reddedilir**); (k) ALTER süresi ölçülmüş ve ≤ 2 sn (ya da `NOT VALID` yolu); (l) `holding_advance_tag_counter` replay korur (eşit/geri ctr → `replay`, sayaç değişmez; holding dışı → `not_peer`).
- **API-1c:** `site_punches` katalogu: GPS/IP/not/`policy_context`/`verdict`/`matched_sid`/e-posta sütunu yok, `outcome` kapalı küme ve **eşleme toplam** (her `verdict` × kanal × onay durumu için ya bir `outcome` ya da açık "yazılmaz"; `ignored` yazılmaz ve KM kaydı geri alınmaz); **red tekilleştirmesi:** aynı replay URL'si N kez → KF'de 1 satır, KM'de N red; farklı çalışanların replay'i ayrı; `ctr`'ı oynanmış 200 sahte URL (`ctr` ≤ ve > `last_ctr` karışık) → plaket + çalışan + saat başına 1 satır; deaktive çalışanın 3 gerçek dokunuşu (`sun_valid`) → 3 satır; kayıp plaketin aynı URL'si N kez → 1 `refused_tag_not_active` ve 1 uyarı; **aynı saatte önce sahte `refused` sonra kayıp plaket → ikisi de KF'de** (uyarı bastırılamaz); kova sunucu saatinden ve oturum saat diliminden bağımsız; **KF raporu holding çalışanları için süre hesaplamaz** — KF→KM ve KM→KF ardışık iki vardiya (aynı gün ve iki güne yayılan) KF'de **0 sa** ve dokunuş listesi; onaylı flag `transaction_id` başına tek satır (son sonuç), sahte açık kayıt yok; KF'nin kendi çalışanlarının süreleri değişmez; `employer_manual` ayrı ve toplamdan hariç; deaktive KM çalışanının KF plaketindeki reddi KF'de yalnız `refused`; kayıp KF plaketi uyarısı **KF panelinde**; KF onay veremez; dokuz okuyucunun her biri saha satırını doğru mekan/vardiya ile (her yönde) gösterir; lokasyon silme holding referansını sayar; C izdüşümden 0 satır.
- **API-2:** (a) düz `INSERT INTO transactions` olay doğurur; (b) stres: ≥ 12 yazıcı, ≥ 3 tenant, **ters yönlü holding dokunuşları**, abort/savepoint karışık, eşzamanlı okuyucu → 0 kaçan, **0 kilitlenme**, `seq` yoğun; mutasyonlar kırmızı: kilitsiz dizi, ayrı işlemde sayaç, **iki ayrı tetikleyiciyle sayaç kilidi** (kilitlenme); (c) geri yükleme doğrulayıcısı; (d) flag/onay/manuel/practice; (e) C görmez; (f) `arwd` altında fiil reddi ve geçici tablo bağlama reddi (her iki definer'ın tetikleyicileri); (g)–(i) tetikleyici hatası, savepoint, zaman aşımları; (j) hedef satırlar ve **anlık görüntü** alanları — **iki hedef satırı da holding tetikleyicisi yazar** (KM@KF: KM satırında KF lokasyon id/ref/ad/şirket kodu, KF satırında KM çalışan **id**/ref/kod/şirket kodu); kendi-tenant tetikleyicisi `site_*` satırına ve ona bağlı onaya **hiç** olay yazmaz; her holding punch'ında işveren olayı **tam bir kez**; **teslim sayımı:** API'si açık ve uç noktası aktif iki tenant'ta KM@KF → 2 teslim (KM `employer`, KF `location`); (k) kapalı API erişimi; (l) geri doldurma ≤ 2 sn; (m) katalog: iki rol, **tetikleyici ad sırası**, EXECUTE yalnız sahipte, `TG_RELID`; K-34'ün `webhook_endpoints.url`/`signing_key_sealed` pinleri; holding tetikleyicisinin yazdığı dört tablonun sütun pini; (n) sıfırdan pod; (o) `tappa_feedwriter` yetkilerinin her biri tek başına zorunlu (yeniden ölçüm); (p) üyelik bitmişken onay → KM olayı **ve teslimi** **holding tetikleyicisinden**, karşı taraf anlık görüntüsü NULL olarak, **tam bir kez** yazılır; KF tarafı atlanır, hata yok; (q) **sayaç satırı eksikken** ters yönlü holding dokunuşları → 0 kilitlenme ("var et" adımı kaldırılmış mutantta kırmızı — 7. turda ölçüldü); doğrulayıcı `count(tenants) = count(feed_counters)`.
- **API-3:** katalog `api_keys`; iptal geri alınamaz; eşzamanlı çift/toplu iptal; 401/403; yanlış önek; log yok; R4/R7/R7b/R7d kasıtlı ihlalde kırmızı; eşit env anahtarıyla başlamaz; C'nin anahtarı A'yı göremez.
- **API-4 / API-4b / API-5 / API-6:** 5. sürümdeki gibi (katalog `api_idempotency`; host kapıları; IP kovası; Idempotency eşzamanlılığı; §4.7 her hücre; C anahtarı başka tenant'ı göremez; e-posta değişimi, resend döngüsü, kuyrukta kod yok, iki pod tek basım, bütçe DB'den ve eşzamanlılıkta tam 1; `GET`'te e-posta yok).
- **API-7:** dört şekil × iki kapsam doğru tenant'a, anlık görüntü alanlarıyla; C hiçbirini görmez; manuel/reddedilmiş yok; `limit` 1001 → 400.
- **API-8:** holding'deki iki tenant, C yok, e-posta yok, `active` var (K-1h), sayfalı (≤ 1000); örtüşme penceresi.
- **API-9:** katalog; tenant başına tek uç nokta; anahtar bir kez; ping/yeniden gönderim kovası; kapalı erişimde ret.
- **API-10:** dört yönlendirme; SSRF ret listesi + iki aşamalı DNS; devre dışı uç noktaya teslim yok; proxy'ye rağmen doğrudan; iki pod tek gönderim; açık işlem yok; kapanış.
- **API-11:** prod derlemesinde simülasyon yok; CI `-tags sandbox`; deploy kapısı; gerçek UID reddi; dokuz `IsHardened` kapısı; operatörsüz anahtar 403; holding simülasyonu.
- **API-12:** OpenAPI her uç/hata; §2'nin 30 maddesi.
- **API-13:** spec §7'nin dokuz maddesi (6. madde sözleşme farkı); güvenlik ONAY; KF+KM holding'i kuruldu; holding dışı izolasyon prod'da örneklendi.

---

## 6. Riskler ve tuzaklar

- **CLAUDE.md §4.5 ikinci istisna** — kapalı fonksiyon listesi + K-33 iki yönlü testi + K-34 sütun pinleri; `holding_members`'a `tappa_app`'in yazabildiği tek yol herhangi bir tenant'ın holding'e katılması olurdu.
- **CLAUDE.md §4.4** — holding ilerletmesi replay ifadesinin ikinci kopyası; R4 şema nitelikli biçimi görecek şekilde genişler; çekişme testi zorunlu (`tags_counter_monotonic` eşit ctr'yi durdurmaz).
- **Kilit sırası** — K-31; ihlali kilitlenme ve kayıp kayıt.
- **Tenant'lar arası takılı yazıcı** — KF'de takılı işlem KM'nin holding dokunuşlarını bekletir (§4.3 bedel).
- **K-1a bedeli** — KF şube istisnaları KM çalışanına uygulanmaz (çapraz QR hariç — K-1g).
- **R7 / R7d** — log ve doküman taraması (bu dosyanın 1. sürümü push'u durdurdu).
- **Tek replika, K-19 kümesi** — yeniden başlamada boşalır.
- **Davet tavanları** ilk toplu senkronu saatlere yayar.
- **Pilot verisi** → K-18 runbook'u.
- **GDPR** — K-1d/f/h görünürlüğü, `employer_manual` satırları ve `site_punches`'ın saklama süresi DPA notunda (kullanıcı).
- **Egress açık** — SSRF kapısı tek savunma.
- **Canlı ön koşul:** OP-15.

## 7. Kullanıcının dış adımları

1. ~~K-1, K-1a…h (K-1e kaldırıldı), K-2, K-3, K-19, K-25, K-26~~ ✅
2. Sandbox için `taptime.mt` dışında bir alan adı + DNS; `api.taptime.mt` DNS (Cloudflare, DNS-only).
3. KF-RMS / KM-ERP canlı + test webhook adresleri — API-13'te.
4. Canlı öncesi: KF ve KM panellerinde lokasyon/çalışan bağlama (K-18).
5. DPA / aydınlatma metni: holding içi paylaşım (K-1d/f/h — sonuç, yön, aktiflik dahil) ve `site_punches` saklama süresi.
6. Canlı ön koşul: OP-15.

## 8. Kapsam dışı (v1)

Punch yazma API'si · punch iptali/düzeltmesi · departman uçları · lokasyon silme API'si · webhook
yönetimi API'den · lokasyon başına saat dilimi · KF'nin KM çalışanı için onay vermesi · ikiden fazla
tenant'lı holding için ek UI · OAuth · paylaşımlı oran deposu · egress NetworkPolicy.

---

## 9. Denetim izi

**1.–5. tur (2026-10-09/10; `a169391` … `690c69b`) — "tek tenant, iki şirket" modeli.** Ayrıntı
`690c69b`'de (git geçmişi). 4. tur üçüncü göz ONAY, 5. tur güvenlik ONAY. Korunanlar: DB
tetikleyicisiyle olay · tenant sayaç satırı imleci (0 kaçan) · §4.0 (`arwd`) · fonksiyon EXECUTE
kuralı · ölçülmüş feedwriter yetkisi · SSRF izin listesi · askı hizası · `IsHardened` · Idempotency ·
pasifleştirme bütçesi.

**Model değişikliği (2026-10-10, kullanıcı):** K-1 holding; K-1a…d; K-3 alt alan (sonra geri döndü).

**6. tur (2026-10-10, `77bd943`) — üçüncü göz RED (9 ORTA, 10 DÜŞÜK), güvenlik RED (6 ORTA, 8 DÜŞÜK).**

| Bulgu | Kaynak | Nereye işlendi (7. sürüm) |
|---|---|---|
| İki ayrı tetikleyici kanonik kilit sırasını kuramıyor → **kilitlenme, kayıt kaybı** (iki mercek de ölçtü) | üçüncü göz O1, güvenlik O-1 | K-31 · §4.3 · API-2 (b)(m) |
| Lokasyonsuz/kayıp KF plaketi CHECK'e çarpıyor; `SameHolding`'in kaynağı tanımsız | üçüncü göz O2, güvenlik D-3 | K-30 kanal CHECK · §4.1 tap yolu · API-1b (j) |
| Manuel kayıt `site_*` taşıyamıyor | üçüncü göz O3 | K-30 · `holding_site_locations` · API-1c |
| K-1c logosu sunulamıyor; liste kapalı değildi | üçüncü göz O4, güvenlik O-6 | `holding_logo` · ADR 0024 notu · API-1b (i) |
| Spec `company` ve karşı tarafın `externalRef`'i üretilmiyor | üçüncü göz O5, güvenlik O-6 | K-22 `api_code` · K-32 anlık görüntü · **K-1f (kullanıcı)** |
| Saat dilimi eşitliği yalnız eklemede | üçüncü göz O6 | K-36 · API-1a |
| İzdüşüm K-1d'yi aşıyor (`matched_sid`/`verdict`, kapanış çıkışı); değişmez çelişkisi | üçüncü göz O7, güvenlik O-5 | kapalı `outcome` kümesi · **K-1e (kullanıcı)** · §4.3 değişmez · API-1c |
| COOKIE-1 kapsamı ve geçişi; alt alandan `same-site` CSRF | üçüncü göz O8, güvenlik O-2 | **K-3 ayrı alan adına döndü (kullanıcı)** · COOKIE-1 kaldırıldı · GATE-1 (K-37) |
| Üyelik bitince onay davranışı | üçüncü göz O9 | §4.3 sessiz atlama · API-2 (p) |
| `tappa_holdingdefiner` matrisi ve "asla SELECT" listesi yok | güvenlik O-3 | K-34 · API-1a |
| Eş süzgeci tek yönlü test | güvenlik O-4 | K-33 · API-1b (b) |
| Geçiş penceresi yalnız erteliyor; `now()` | güvenlik D-2 | K-29 katı/toleranslı model · `clock_timestamp()` · API-1b (g) |
| Çağrı yeri `sun.Verify` değil `checkin.advance` | üçüncü göz D1, güvenlik D-4 | §4.1 tablo |
| EXECUTE çelişkisi; `TG_RELID` | güvenlik D-1 | §4.0 · §4.3 |
| Çapraz QR KM politikasıyla KF verisine giriyor | güvenlik D-7 | **K-1g (kullanıcı)** · API-1b (f) |
| `ctr_gap`; yedi KM okuyucusu; ALTER maliyeti; API-0 listesi; bağımlılık; `punch_events` FK; feedwriter listesi; "Bedel" paragrafı; tek üyelik / yeniden katılım; R4 şema nitelikli; bekleyen aktivasyon | üçüncü göz D2–D10 | §4.1 · §4.3 · §5 · K-35 · §4.2 R4 |
| Rehber sınırsız; panel çerez yolu; saklama süresi | güvenlik D-5, D-6, D-8 | `holding_directory` sayfalı · (COOKIE-1 kalktı) · §7 madde 5 |

**7. tur (2026-10-10, `71b8dc6`) — üçüncü göz RED (5 ORTA, 7 DÜŞÜK), güvenlik RED (4 ORTA, 8 DÜŞÜK); bloklayan yok.**
Ölçülenler: K-31 tek ön-kilit 14 288 işlemde (8 106 holding) 0 kilitlenme / 0 kaçan, ön-kilitsiz
mutant 20'de 9 kilitlenme; prototipte K-33 doğru süzgeç, K-34 18 sütun pini, logo kapısı,
tetikleyici bağlama reddi, 3 tur yarışta tam 1 kazanan.

| Bulgu | Kaynak | Nereye işlendi (8. sürüm) |
|---|---|---|
| Holding punch'ında işverenin satırındaki karşı taraf sütunlarını feedwriter RLS yüzünden okuyamıyor | üçüncü göz O-1 | §4.3: iki hedef satırı holding tetikleyicisi yazar · API-2 (j) |
| `punch_events`'te `employee_id` yok | üçüncü göz O-2 | K-32 · §4.3 · §2 madde 27 |
| K-1g guardrail sırası | üçüncü göz O-3, güvenlik D-b | K-38 |
| K-29 tek pencere: NFC'de sahte replay, QR'da 60 sn yeni yazma, okumada `static_ips` | üçüncü göz O-4, güvenlik R6 | K-29 kanal × zaman modeli · §4.1 · API-1b (g) |
| `site_punches` `ignored`/K-1e/manuel temsil edemiyor; kapatılan girişin kaynağı | üçüncü göz O-5, güvenlik D-h, R5/R7, R5 manuel | toplam `outcome` eşlemesi · `closes_transaction_id` · K-1e yalnız izdüşümde · `employer_manual` · API-1c |
| K-33 testi M3'ü yakalamıyor (ölçüldü) | güvenlik R5 | K-33 H2 fikstürü + M1/M2/M3 · API-1b (b) |
| Sayaç satırı eksikken kilitlenme (ölçüldü) | üçüncü göz D-1 | K-31 "var et" + assert · §4.4 · API-2 (q) |
| Okuyucu listesi eksik (9) | üçüncü göz D-2 | §4.1 |
| Bağımlılıklar; rehber sayfalı | üçüncü göz D-3, D-4 | §5 · §2 madde 29 |
| GATE-1 aktivasyon `strict=false` dalı | üçüncü göz D-5, güvenlik D-f | K-37 · GATE-1 kabul |
| Kayıp plaket uyarısı yanlış tenant'ta | üçüncü göz D-6 | §4.1 · API-1c |
| K-34 tetikleyici yolu; "asla SELECT" eksikleri | üçüncü göz D-7, güvenlik D-d | K-34 |
| `site_tenant_id` NULL iken `site_*` (ölçüldü) | güvenlik D-a | K-30 · API-1b (j) |
| K-1d ters yön (`static_ips`, `ip_match`, GPS mesafesi) | güvenlik D-c | K-39 |
| K-36 tetikleyicisi kapalı listede değil | güvenlik D-e | K-36 · §4.1 tablo |
| K-35 belirsiz | güvenlik D-g | K-35 |
| `outcome`/`type`/`active` K-1d dışında | güvenlik D-h | **K-1h (kullanıcı)** |

**8. tur (2026-10-10, `62519e5`) — üçüncü göz RED (3 ORTA, 6 DÜŞÜK), güvenlik RED (2 ORTA, 7 DÜŞÜK); bloklayan yok.**
Ölçülenler: "var et" adımıyla eksik sayaç satırında 120 turda 8 640 işlem 0 kilitlenme (mutant
40 turda 324); üç sonuçlu ilerletme tek CTE'de; ikinci AFTER tetikleyicisi birincinin yazdığını
görüyor; K-29 modeli çekirdekte doğru; `refused` nedeni sızdırmıyor; GATE-1 meşru akış kırmıyor.

| Bulgu | Kaynak | Nereye işlendi (9. sürüm) |
|---|---|---|
| `SameHolding`'in kaynağı çelişkili, ilerletilmeyen dallarda tanımsız | üçüncü göz ORTA-1, güvenlik D-1 | K-29 tek kaynak · API-1b (g) |
| İki tetikleyici arasında "atlandı" sinyali yok (çift/kayıp olay) | üçüncü göz ORTA-2, güvenlik D-3 | §4.3: holding tetikleyicisi işveren olayını koşulsuz yazar · API-2 (j)(p) |
| Manuel çıkış K-1e dışında | üçüncü göz ORTA-3 | §4.1 manuel `closes_transaction_id` · API-1c |
| `closes_shift` karar/zaman/tekillik süzgeçsiz (ölçüldü) | güvenlik ORTA-1, üçüncü göz D6 | §4.1 yazma şartları + kısmi UNIQUE + DB doğrulaması · API-1c |
| Eşlik mutantları tetikleyicileri kapsamıyor (ölçüldü) | güvenlik ORTA-2 | K-33 tek `holding_peer` yüklemi · API-1b (b) |
| Tolerans DB'de zorlanmıyor; K-1e eşliği | güvenlik D-2 | K-29 (`sun_valid` + `last_ctr ≥ ctr`) |
| Üç sonucun ayrımı | üçüncü göz D1 | §4.1 tablo |
| GATE-1 üç tanım | üçüncü göz D2, güvenlik D-4 | K-37 · §4.8 · GATE-1 kabul |
| API-1c/API-2 sınırı | üçüncü göz D3 | §5 |
| K-34 eklemeleri kabulde yok; tetikleyici pini kapsamı | üçüncü göz D4, güvenlik D-5 | API-1a · API-2 (m) · K-34 |
| API-0 listesi, GDPR maddesi eski | üçüncü göz D5 | §5 · §6 |
| `employer_manual` bedeli | güvenlik D-6 | K-1a · KF raporu toplamdan hariç |
| Replay reddi debounce'suz KF'ye | güvenlik D-7 | `site_punches` kısmi UNIQUE (tag, ctr) |

**9. tur (2026-10-10, `bea8ac3`) — üçüncü göz RED (3 ORTA, 11 DÜŞÜK), güvenlik RED (1 ORTA, 6 DÜŞÜK); bloklayan yok.**
Ölçülenler: 8. turun iki ORTA'sı kapandı (K-1e yazma şartları; tek `holding_peer` ile M1/M2/M3
fonksiyonlarda **ve** tetikleyicide kırmızı, KM manuel + C lokasyonu ret); K-29 toleransı DB'de
doğru; eşlik bitince onayda KM olayı tam bir kez; `refused` replay'i KF'de 1, KM'de 5.

| Bulgu | Kaynak | Nereye işlendi (10. sürüm) |
|---|---|---|
| Kapanış bağının zaman sırası kontrolü geriye beyanlı çıkışı ve geriye dönük manuel düzeltmeyi **her tenant'ta** yazılamaz kılıyor (ölçüldü) | üçüncü göz ORTA-1, güvenlik ORTA-1 | **K-1e kaldırıldı (kullanıcı)** — `closes_transaction_id`/`closes_shift` yok |
| "İlk kapanış kazanır" ADR 0011 düzeltmesini taşımıyor (ölçüldü) | üçüncü göz ORTA-2 | K-1e kaldırıldı; KF raporu izdüşüm içi ADR 0008/0011 eşlemesi |
| İşverenin teslim satırı yazarsız (gerileme) | üçüncü göz ORTA-3 | §4.3 olay **ve teslim** · API-2 (j)(p) |
| `ON CONFLICT` eksik, tekillik delikleri, manuel kapanış toplamda, `closes_shift` biçimi | üçüncü göz D1, D7, D8; güvenlik D-1, D-3, D-6 | K-1e ile kalktı; red eklemeleri `ON CONFLICT DO NOTHING`; KF raporu yalnız `counted`/`approved` |
| §4.1'de `SameHolding` cümleleri; tetikleyici satırı eski; onayda tolerans | üçüncü göz D2, D3 | §4.1 · K-29 (onaylar katı) |
| "Koşulsuz" K-4 ile çelişiyor | üçüncü göz D4 | §4.3 "K-4 şartını sağlayan, eşlikten bağımsız" |
| `holding_peer` kapalı listede değil; kâhin | üçüncü göz D5, güvenlik D-4 | §4.1 tablo · API-1b (g) |
| Red tekilleştirmesi yarım; anahtar saldırganın elinde | üçüncü göz D6, güvenlik D-2 | `site_punches` iki kısmi UNIQUE (`employee_id`; denenmemişte saat kovası) · API-1c |
| Dördüncü ilerletme sonucu; replay reddinin çıkış yarışı | üçüncü göz D9, güvenlik D-5 | `peer_inactive` · K-29 red toleransı |
| K-3 satırı eski; QR yarış penceresi | üçüncü göz D10, D11 | K-3 · K-29 adlandırılmış yarış |

**10. tur (2026-10-10, `e243e5b`) — üçüncü göz RED (2 ORTA, 9 DÜŞÜK), güvenlik RED (1 ORTA, 5 DÜŞÜK); bloklayan yok.**
Ölçülenler: K-1e kalkınca kayıt kaybı kapandı (geriye beyanlı NFC çıkışı, 3 gün geriye manuel
düzeltme, geriye beyanlı holding çıkışı — 5/5 yazıldı); `holding_peer` `tappa_app`'ten
çağrılamıyor, INVOKER olduğu için yanlışlıkla açılsa da `holding_members` okunamıyor; dört
ilerletme sonucu doğru sırada (20 eşzamanlı → 1 `advanced` / 19 `replay`); tetikleyici durum
tablosunda çift yazma yok; aynı sahada gece vardiyası doğru.

| Bulgu | Kaynak | Nereye işlendi (11. sürüm) |
|---|---|---|
| KF raporunun izdüşüm içi eşlemesi sahte aralık sayıyor (ölçüldü: 9 sa, 32 sa) | üçüncü göz ORTA-1, güvenlik ORTA-1 | KF raporu holding çalışanları için **süre hesaplamaz** · §2 madde 30/31 · API-1c |
| Red tekilleştirmesinin ayırıcısı/kova kaynağı; `outcome` anahtarda yok (ölçüldü: 500/200 satır; uyarı bastırma) | üçüncü göz ORTA-2, D1; güvenlik D-1, D-2 | `sun_valid` ayrımı + sunucu saatli kova + `outcome` anahtarda · API-1c |
| İnaktif red toleransı (ölçüldü) | üçüncü göz D2, güvenlik D-3 | K-29 red kolu `ctr` şartsız |
| `peer_inactive` karar girdisi bayat | üçüncü göz D3 | ilerletme güncel durumu döner · API-1b (g) |
| K-29 "üç sonuç" artığı | üçüncü göz D4 | K-29 |
| `holding_peer` §4.0 çelişkisi, sahibi | üçüncü göz D5, güvenlik D-5 | §4.0 istisnası · API-1b (g) |
| §4.3 sıra (kilit olaydan önce) | üçüncü göz D6 | §4.3 |
| BEFORE/AFTER iki ayrı zaman (ölçüldü) | üçüncü göz D7, güvenlik D-4 | `statement_timestamp()` · AFTER aynı tolerans |
| Onaylı flag katlanmıyor | üçüncü göz D8 | `transaction_id` başına son sonuç |
| Eşlik bitince onay KF'ye düşmüyor | üçüncü göz D9 | adlandırılmış bedel |

**11. tur:** bekliyor.
