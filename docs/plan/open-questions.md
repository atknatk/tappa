# Açık sorular ve bulgular

İki bölüm: **kötüye kullanım bulguları** (A/Y — plan denetiminde çıktı, çoğu
göreve dönüştü) ve **açık sorular** (Q — karar bekliyor).

**Cevaplanınca:** durumu `cevaplandı` yap, kararı tek cümleyle yaz, kalıcı sonucu
varsa bir ADR'ye veya ilgili görev kartına taşı.

Sahip: **A** = Atakan (ürün/ticari/hukuki karar) · **C** = Claude (teknik öneri
hazırlar, onay A'da).

---

## Bölüm 1 — Kötüye kullanım bulguları

2026-07-24 denetiminde çıkanlar: *"personel gerçekten geldi mi, gerçekten çıktı
mı"* sorusunu kimin nasıl atlatabileceği.

### Ciddi açıklar

| # | Bulgu | Sömürü | Durum |
|---|---|---|---|
| **A1** | **URL biriktirme.** `GET /t` sayacı ilerletmez (doğru UX kararı), çip ise her okumada `ctr`'ı artırır. SUN payload'ında zaman yok → URL "dokunuş oldu"yu kanıtlar, "**şu an** oldu"yu değil. | **Uçak modunda** plakete 10 kez dokun — çip `ctr`'ı artırır, URL adres çubuğunda görünür, istek sunucuya hiç ulaşmaz. Ertesi gün evden URL'yi aç → `GET` *o an* kaydedilir → 5 sn sonra POST → tazelik penceresi içinde → geçer. Düşük trafikli plakette çalışır (yoğunda biriktirilenler başkasının tap'iyle ölür). | ⚠️ **ÇÖZÜLMEDİ — kabul edilen risk.** İlk işaretlemem yanlıştı: [M5-10](m5-tap-akisi.md) tazelik penceresi *farklı* bir tehdidi kapatıyor. Azaltma (Q21): `tap:ctrGap` + `base:ctr-gap-review` politikası, kaynak kapsamlı. Kayıt: ADR 0005 ([M3-09](m3-policy-motoru.md)) |
| **A2** | **QR + sahte GPS.** QR statiktir, fotoğraflanır, süresiz geçerlidir. Karar tablosu QR'da "IP **veya** GPS" istiyor. | Hiç işyerine gelmeden `ok` + APPROVED. NFC'de en azından bir kez fiziksel dokunuş gerekiyor; QR'da o bile yok. | ✅ **Q15 cevaplandı:** `base:qr-requires-ip` — QR'da IP zorunlu, GPS yetmez |
| **A3** | **GPS sahtelenebilir.** Mock location Android'de root gerektirmiyor. GPS-only tap → `ok`, trust 50, APPROVED damgası. | Web'de konum doğrulaması yok; attestation uygulama kurulumu ister, bu da app-less vaadine aykırı. GPS yolunda "oradaydı" kanıt değil **beyan**. | ✅ **Q16 cevaplandı:** `base:gps-only-allow` tenant anahtarı, varsayılan `ok` + [M6-11](m6-dashboard.md) oran raporu |
| **A4** | **Buddy punching çözülmüyor.** Maria telefonunu Joseph'e verir → dört kanıtın dördü de geçer, trust 100, APPROVED. | Yerine geçtiğimiz parmak izi sisteminin **çözdüğü** tek şey. handoff §2 bu üstünlüğü hiç anmıyor; müşteri ilk demoda soracak. | ✅ **Q19 cevaplandı:** kabul edilen risk (ADR 0005, [M3-09](m3-policy-motoru.md)) + tespit sinyali [M6-11](m6-dashboard.md) |

### Yapısal boşluklar

| # | Bulgu | Durum |
|---|---|---|
| **Y1** | **Çapraz lokasyon tap'i tanımsız.** Maria St Julians'a kayıtlı, Hamrun'da basıyor (zincirde normal). Geç kalma hangi vardiyaya göre — profil mi, tap edilen lokasyon mu? Yanlış seçim her gün yanlış "geç kaldı" üretir. | ✅ **Q17 cevaplandı:** tap edilen lokasyonun vardiyası + not; departman vardiyası yine ezer |
| **Y2** | **Çapraz tenant tap'i tanımsız.** İlk kapanışım yüzeyseldi: `sys:no-session`'a bağlamak yetmiyor, çünkü **oturum araması tenant kapsamlı olamaz** — tag UID de çerez de tenant taşımıyor, tenant tam da o iki aramanın sonucu. `token_hash` global UNIQUE olduğu için arama **başarılı olur** ve elde `Employee{KM}` + `Tag{KF}` kalır. | ✅ Yeniden çözüldü: `sys:tenant-mismatch` guardrail'i (sıra 1, [M3-05](m3-policy-motoru.md)) + M1-01 ADR'sine **tenant çözümleme istisnası** (madde 7) + `db/queries/resolve.sql` ayrımı ([M1-08](m1-veri-katmani.md)) |
| **Y3** | **FLAGGED onayı `transactions`'a ikinci satır yazıyor** → tablo anlamı bulanıklaşıyor, saat toplamı şişebilir. | ✅ **Q20 cevaplandı:** ayrı `transaction_reviews` tablosu ([M1-06](m1-veri-katmani.md), [M6-04](m6-dashboard.md)) |
| **Y4** | **IP kanıtı pratikte çalışmayabilir.** IP 50 puan (GPS'in 30'undan fazla) ama şartı: telefon **mekânın WiFi'ında** olmalı. Planda "çalışan WiFi'a bağlanır" adımı hiç yoktu. Misafir WiFi ayrıca binanın dışını da kapsar. | ✅ **Q14 cevaplandı:** onboarding'e WiFi adımı ([M5-02](m5-tap-akisi.md)), pilotta oran ölçümü ([M8-06](m8-deploy-pilot.md)) |
| **Y5** | **Unutulan çıkış için politika yok.** Otomatik kapatma var mı, açık gün kaç saat sayılır? Restoranda haftalık olay. | ✅ **Q18 cevaplandı:** otomatik kapatma YOK; açık kayıtlar anomali listesinde, müdür manuel çıkış girer |
| **Y6** | **Oran sınırı §4.6 ile çelişiyordu** ("aşımda kayıt yazılmaz"). | ✅ Düzeltildi — [M5-03](m5-tap-akisi.md): sınır meşru tap'in değemeyeceği kadar geniş, aşanlar `audit_log`'a |
| **Y7** | **Çevrimdışı kuyrukta `occurred_at` sınırsız** — istemci istediği saati iddia edebilir. | ✅ `base:queued-window` politikası olarak [M3-06](m3-policy-motoru.md)'da |

### İkinci denetim — dış göz (2026-07-24)

Plan üç bağımsız ajana okutuldu (tutarlılık · güvenlik · pratiklik). Yukarıdaki
A/Y maddelerine ek olarak çıkanlar ve nereye işlendikleri:

| # | Bulgu | Durum |
|---|---|---|
| **K1** | `occurred_at` istemciden geliyor ve hiçbir guardrail sınırlamıyor. Zaman dört kanıtın **hiçbirinde** yok; yani mesai kaydının en önemli alanı tek doğrulanmamış girdi. Sınırı yalnız `base:queued-window`'a bırakmak yetmez — o baseline, tenant kapatabilir. | ✅ `sys:occurred-at-bound` guardrail'i (sıra 5) + `tap:occurredAtSkewSeconds` anahtarı + [M5-05](m5-tap-akisi.md) |
| **K2** | Motor yetkilendirmeye de bakıyor ama varsayılan `review` → **fail-open**. `policy:edit` için "review" anlamsız; yeni tenant'ta hiç yetki politikası bağlı olmadığı için herkes politika düzenleyebilir. Guardrail listesinde motoru koruyan tek satır yoktu. | ✅ Eylem ad alanına göre **iki varsayılan** (`tap:*` → `review`, diğerleri → `deny`) + `sys:policy-edit-owner-only`, `sys:no-self-review` guardrail'leri |
| **K3** | Tenant çözümlemesi RLS bağlamından **önce** gelmek zorunda; "her erişim `WithTenant`" kuralıyla çelişiyor. Çözülmezse ya bypass eklenir ya tap hiç çalışmaz. | ✅ M1-01 ADR madde 7 (dar, adlandırılmış, testli istisna) |
| **Y-B** | `ignore` effect'i tenant'a açıktı ve öncelik sırasında hiç yoktu. *"22:00'den sonra `ignore`"* politikası gece vardiyasını sessizce ödenmez kılardı — §4.6'nın aynadaki hâli. | ✅ `ignore`/`redirect` **yalnız guardrail**; doğrulama tenant/baseline belgesinde reddediyor; öncelik `deny > ignore > review > allow` |
| **Y-C** | `transaction_reviews`'a kısıt yoktu: tekrar onay, `ok` kayda "rejected" yazıp saat silme, kendini onaylama. Q20 `transactions`'ı korudu ama etkin durumu sınırsız değiştirilebilir kıldı. | ✅ `UNIQUE (transaction_id)` + yalnız `verdict='flag'` hedefi + `reviewer_id <> employee_id` ([M1-06](m1-veri-katmani.md)) |
| **Y-D** | **Müdür kimlik basabiliyor:** davet kodu panelde görünüyor → sahte profil → kendi telefonunun ikinci profilinde aktivasyon → her gün trust 100. Buddy punching değil: çalışan hiç yok, iş birliği gerekmiyor. | ⚠️ Kabul edilen risk (ADR 0005) + tespit sinyali [M6-11](m6-dashboard.md) + Q02 çözülünce kod çalışanın kendi kanalına ([M5-02](m5-tap-akisi.md)) |
| **Y-E** | **Mekânda bırakılmış proxy:** WiFi'da prizde duran ucuz telefon + VPN → evden gelen tap'in kaynak IP'si mekânın IP'si → `ok`, trust 70. GPS-only oranında **görünmez**, tersine kayıtlar "en güvenilir" görünür. | ⚠️ Kabul edilen risk (ADR 0005) + `tap:gpsConflict` anahtarı + `base:gps-conflict-review` + [M6-11](m6-dashboard.md) metriği |
| **Y-F** | Sekiz guardrail arasında **sıra tanımsızdı** — §5'in normatif "ilk eşleşen kazanır" garantisi M3'e taşınırken düşmüş. Yanlış sıra: bilgi sızıntısı, müdüre push seli, replay penceresi. | ✅ M3-05'te 1→10 **normatif sıralı liste** + M3-08'de sıra testi |
| **Y-G** | §5 satır 1–2, satır 3'ten önce geldiği için **oturumsuz reject kaydı** yazılmak zorunda; `employee_id` NOT NULL varsayımıyla INSERT patlar → §4.6 tam da var oluş senaryosunda ihlal. | ✅ `employee_id`/`location_id`/`department_id` nullable + CHECK ([M1-06](m1-veri-katmani.md)) |
| **Y-İ** | `practice` (ve `channel`) istemci gövdesinden geliyordu. `practice=true` ile çıkış kaydını zincirden düşürüp 3 saati 11 saat göstermek mümkün. | ✅ İkisi de **sunucuda türetilir** ([M4-06](m4-tap-motoru.md)) |
| **Y-J** | Policy motoru için nicel sınır yoktu; tek VPS'te tek process — 200 belge × 500 ifade yazan bir tenant tüm tenant'lar için servisi durdurabilirdi. | ✅ M3-03'e belge/ifade/kota sınırları; M9-06 simülatörü aralık sınırlı ve arka planda |
| **Y-K** | "En kısıtlayıcı kazanır" + kaynak kapsamı birlikte **dar istisna yazmayı imkânsız** kılıyordu; müşterinin tek gevşetme yolu korumayı 9 şubede birden kapatmaktı. | ✅ **Spesifik kaynak, genel kaynağı ezer** ([M3-04](m3-policy-motoru.md)) |
| **Y-L** | `TAPPA_GPS_RADIUS_M` ve `TAPPA_DEBOUNCE_SECONDS` yalnız `> 0` kontrolünden geçiyor. `=20000000` tek satırla tüm parkta *proof of place*'i sessizce kapatır. | ✅ M3-05 kabul kriteri: config guardrail'in ilan ettiği **aynı sabitlerden** okur, aralık dışı → başlangıçta hata |
| **P1** | Yasal metinler (GDPR Art. 13, DPA, gizlilik, imprint) planda **hiç yoktu**. | ✅ Q23 — dağıtıldı + [M8-06](m8-deploy-pilot.md) pilot kapısı |
| **P2** | Admin kullanıcı şeması yoktu; M6-01 gizlice bir migration taşıyordu. | ✅ Yeni görev [M1-11](m1-veri-katmani.md) |
| **P3** | Tahsilat hiçbir yerde yoktu — "MVP dışı" listesinde bile. | ✅ Q24 — yeni görev [M6-12](m6-dashboard.md) + roadmap kapsam dışı notu |
| **P4** | Üretim tenant'ını kimin kuracağı tanımsızdı; seed açıkça sahte. | ✅ Yeni görev [M8-07](m8-deploy-pilot.md) |
| **P5** | Simülatör mevcut şemayla çalışamaz: bağlam anahtarları `transactions`'a yazılmıyor. | ✅ `policy_context jsonb` ([M3-07](m3-policy-motoru.md)), **1. günden** yazılır |
| **P6** | iPhone X ve öncesi arka planda NFC okuyamaz → kalıcı QR → Q15 gereği IP zorunlu → her gün flag. | ✅ [M5-08](m5-tap-akisi.md) notu + [M8-07](m8-deploy-pilot.md) telefon envanteri |
| **P7** | `router.go`'daki "bounded by cfg.TrustedProxies" yorumu **bugün yanlıştı** (chi RealIP koşulsuz güvenir). | ✅ Kod yorumu düzeltildi ([internal/httpx/router.go](../../internal/httpx/router.go)) |
| **P8** | Mekanik tutarsızlıklar: ctr eşiği iki yerde farklı · mermaid'de M2→M4 kenarı yok · "8 tablo" · M6 "dört sekme" · `sys:no-session` effect'i · kırık çapa · UID örnekleri 8 hane · `K_sdmfilekey`/`K_sdmfileread` · CLAUDE.md "üçünden biri" · state.md'de çift paragraf. | ✅ Hepsi düzeltildi |
| **P9** | Kalanlar: `Makefile` `seed` hedefi `psql` istiyor ama ön koşullarda yok · `govulncheck@latest` pinleme ilkesiyle çelişiyor · `sqlc.yaml`'daki `inet` override'ı skaler, Q07 dizi istiyor · `redline-check.sh` R5 beş unsurdan yalnız dördünü tarıyor. | ⏳ **Q25** |

> **Neden çoğu "politika" oldu:** bu kararların doğru cevabı müşteriden müşteriye
> değişiyor. Statik IP'si olmayan bir tesiste "GPS yetmez" demek sistemi
> kullanılamaz kılar; kamera altında çalışan bir zincirde ise doğru karardır.
> [Policy motoru](m3-policy-motoru.md) bu yüzden var — kararı koda gömmek yerine
> müşteriye veriyoruz, ama **kırmızı çizgileri guardrail katmanıyla kilitleyerek**.

---

## Bölüm 2 — Açık sorular

| ID | Soru | Bloklar | Sahip | Durum |
|---|---|---|---|---|
| Q02 | **E-posta sağlayıcısı.** Davet linki, şifre sıfırlama, rapor gönderimi buna bağlı. Postmark / Resend / SES / kendi SMTP. AB bölgesi ve GDPR işleme sözleşmesi şart. | M5-02, M7-04 | A | açık |
| Q03 | **Admin şifre hash'i.** stdlib'de uygun KDF yok. Öneri: `golang.org/x/crypto/bcrypt` veya `argon2id`. CLAUDE.md §1 gereği yeni bağımlılık onay ister. | M6-01 | C→A | **bcrypt** (2026-07-26) — aşağı bak |
| Q05 | **SDM mirroring modu.** Plain (UID + ctr açık) mı, şifreli PICC data mı? Karar ADR 0003 olacak. | M2-01 | C→A | **plain** (2026-07-26) — aşağı bak |
| Q06 | **Etiket anahtar stratejisi.** Plaket başına rastgele mi, master'dan UID ile türetilmiş mi? Türetme encode'u kolaylaştırır ama master sızarsa tüm park düşer. | M2-01/M2-05 | C→A | **plaket-başına rastgele** (2026-07-26) — aşağı bak |
| Q07 | **`locations.static_ips` tipi.** `inet[]` (tek IP = /32) mi `cidr[]` (aralık) mi? Müşteri ISS'i /29 blok verirse aralık gerekir. | M1-03 | A | **cidr[]** (2026-07-25) — aşağı bak |
| Q08 | **Domain + marka tescili.** `tappa.mt` / `tappa.io` alınmadı, EUIPO taraması yapılmadı. SUN URL'sinde geçiyor. | M8-05, M5-01 (ölçüm), M8-02 | A | açık |
| Q09 | **VIES doğrulaması MVP'de zorunlu mu?** Servis sık kesilir. Öneri: format zorunlu + VIES "en iyi çaba", başarısızsa `vat_verified=false` ve panelde uyarı. | M7-02 | C→A | açık |
| Q10 | **Etiket tedarikçisi ve encode akışı.** Ölçekte encode'lu tedarikçi mi, kendimiz mi? Anahtar teslimi ve döndürme nasıl? | M0 (sipariş), M8-05 | A | açık |
| Q11 | **iOS Safari çerez ömrü.** Oturum 1 yıl hedefleniyor; Safari ITP altında gerçek bir iPhone'da NFC → Safari akışıyla **ölçülmeli**. "Telefon seni tanır" vaadi buna dayanıyor. **M5-01 (`a71e1b2`) kodu tamamlandı ve bunu BLOKLAMIYOR:** sunucu tarafı ölçümden bağımsız (`Set-Cookie`, `httpOnly`, `SameSite=Lax`, `Max-Age=31536000`) ve sonuç kod tasarımını değiştirmez, o yüzden kabul kriteri olamaz (kart düzeltmesi, 2026-07-31). Ölçülecek olan: **sunucunun yazdığı httpOnly** çerez ITP altında 1 yıl yaşıyor mu (7 günlük kırpma JS ile yazılan çerezlere ait — doğrulanması gereken bu ayrım). Gerçek cihaz turu M8-05'te. | hiçbir şeyi bloklamıyor (bilgi: M8-05) | C | **açık — kullanıcı ölçümü bekliyor** |
| Q12 | **Barındırma.** VPS sağlayıcı ve managed Postgres, AB bölgesi, yedek politikası, ~€30-50 bütçe. | M8-02 | A | açık |
| Q13 | **GDPR silme talebi × immutable `transactions`.** Öneri: `employees` üzerinde anonimleştir, `transactions` korunur — hukuki onay ister. Saklama süresi de burada. | M8-06 | A | açık |

## Cevaplananlar

### Q05 — SDM mirroring modu: plain (2026-07-26)

**Karar:** NTAG 424 DNA **plain SDM mirroring** — tap URL'si UID + ctr'yi **açık**
taşır (`?tag=<uid>&ctr=<ctr>&cmac=<mac>`). Şifreli PICC data terk edildi. ADR 0003
(M2-01) normatif yazar.

**Gerekçe:** UID zaten fiziksel plakette basılı ve sistem onu **public** kabul ediyor
(`resolve_tag_by_uid`, uid tap URL'sinde). Şifreli PICC'in mahremiyet kazancı bu yüzden
düşük; buna karşılık PICC'i çözmek için **paylaşılan bir meta-read anahtarı** gerekir
(uid bilinmeden çözülemez → Q06'nın "plaket-başına rastgele"siyle çelişir, tek-nokta
risk). Plain mod basit ayrıştırma + paylaşılan anahtar yok; güvenlik CMAC (proof of
moment) + monoton ctr (replay) 'den gelir. Kullanıcı plain'i seçti.

**Etkilenen:** [M2-01](m2-sun.md) ADR 0003 · [M2-03](m2-sun.md) URL ayrıştırma
(`tag`/`ctr`/`cmac` parametreleri, 7-byte UID hex / 3-byte ctr big-endian / 8-byte cmac) ·
[M2-04](m2-sun.md) (SV2 = `3C C3 00 01 00 80 || UID || ctr`).

### Q06 — Etiket anahtar stratejisi: plaket-başına rastgele (2026-07-26)

**Karar:** her plaket için **bağımsız rastgele AES-128 anahtarı** (K_SDMFileRead);
`TAPPA_TAG_KEK` ile sarmalanıp `tags.aes_key_ref`'te (bytea) saklanır. Master'dan
UID-türetme terk edildi.

**Gerekçe:** izolasyon — bir anahtar sızarsa **yalnız o plaket** düşer (§4.7 ruhu).
Master-türetme encode'u kolaylaştırır ama **master sızarsa tüm park düşer** (Q06 notunun
açıkça uyardığı kabul edilemez tek-nokta risk; güvenlik ürünü için uygun değil). Şema
zaten per-tag `aes_key_ref` taşıyor; encode runbook (M8-05) her tag'in anahtarını üretip
sarmalar.

**Etkilenen:** [M2-01](m2-sun.md) ADR 0003 (anahtar üretimi + KEK sarmalama byte düzeni) ·
[M2-05](m2-sun.md) Wrap/Unwrap (AES-GCM, TAPPA_TAG_KEK) · [M8-05](m8-deploy-pilot.md) encode
runbook (per-tag rastgele üretim). Plain SDM (Q05) ile tutarlı: per-tag anahtar plain modda
sorunsuz (paylaşılan meta-anahtar gerekmez).

### Q03 — Admin şifre hash'i: bcrypt (2026-07-26)

**Karar:** admin şifreleri **`golang.org/x/crypto/bcrypt`** ile hash'lenir.
`password_hash text` (`$2a$…` biçimi). argon2id terk edildi.

**Gerekçe:** bcrypt olgun, basit ve doğru uygulaması en kolay (`GenerateFromPassword`/
`CompareHashAndPassword` hazır, cost-tunable). argon2id modern/memory-hard (OWASP #1) ama
param tuning + `$argon2id$…` hash-string encoding'i elle yazılır → daha fazla kod ve param
tuzağı. Bu ölçekte (tek VPS, panel girişi) bcrypt yeterli ve daha az tuzaklı; §1 "stdlib >
küçük kütüphane" ethos'uyla uyumlu.

**Bağımlılık (§1):** `golang.org/x/crypto` (bcrypt alt paketi). **M1-11'de EKLENMEZ** —
migration KDF-agnostiktir (`password_hash text` her iki KDF'i de tutar). Bağımlılık, hashing
kodu yazılırken **M6-01** (admin auth) ve **M7-04** (şifre sıfırlama) 'te eklenir (§1 "gerektiğinde
ekle" ruhu). Not: bcrypt 72-byte parola sınırı — M6-01/M7-04 uzun parolayı reddeder ya da SHA-256
ön-hash uygular; kararı o kodda.

**Etkilenen:** [M1-11](m1-veri-katmani.md) (`password_hash text NOT NULL`, KDF-agnostik) ·
[M6-01](m6-dashboard.md) (bcrypt hashing + x/crypto dep) · [M7-04](m7-portal.md) (sıfırlama).

### Q07 — `locations.static_ips` tipi `cidr[]` (2026-07-25)

**Karar:** `locations.static_ips` **`cidr[]`**. `inet[]` terk edildi.

**Gerekçe:** `cidr[]` hem tek IP'yi (`/32` olarak) hem ISS bloğunu (`/29` gibi) tek
tiple ifade eder; proof-of-place eşleşmesi **içerme** operatörüyle yapılır:
`@src::inet <<= ANY(static_ips)` — tek IP ve blok ikisi de çalışır. `inet[]` yalnız
tek adres tutabilir; ISS blok verirse her adresi tek tek yazmak gerekir (kırılgan).
Kullanıcı `cidr[]`'i seçti.

**Etkilenen:**
- [M1-03](m1-veri-katmani.md): `locations.static_ips cidr[]`.
- **Q25(c)** ([M1-03](m1-veri-katmani.md)): `sqlc.yaml`'a `cidr[]` override'ı eklenir.
  sqlc v1.28 + pgx/v5 `cidr`i `netip.Prefix`e, `cidr[]`i `[]netip.Prefix`e eşler;
  override gerekiyorsa tam yol `net/netip.Prefix` (mevcut `inet → net/netip.Addr`
  satırının kardeşi). Eklenirse [M0-04](m0-bootstrap.md) override tablosu da
  güncellenir ("sabit olan sayı değil, listedir").
- Eşleşme sorgusu (M1-08 `GetLocationByIP`): `@src::inet <<= ANY(static_ips)`.

### Q27 — RLS politikaları `NULLIF` sarmalayıcısı kullanır (2026-07-24)

**Karar:** her tenant politikası
`NULLIF(current_setting('app.tenant_id', true), '')::uuid` yazar. Çıplak biçim
**terk edildi**. Biçim ADR 0002'de normatif olarak yazılacak
([M1-01](m1-veri-katmani.md)).

**Ölçülen olgu (M0-03, üç ajan tarafından bağımsız üretildi).** `app.tenant_id`
GUC'una bir kez **yazıldıktan** sonra bağlantıda asla `NULL`'a dönmez, `''` olur.
`ROLLBACK`, `RESET app.tenant_id` ve `DISCARD ALL` üçü de `''` bırakır; yalnız
**yeni bağlantı** `NULL`'a döndürür. Tetikleyici **yazmadır** — yalnız *okumak*
placeholder yaratmaz: taze bir bağlantıda üç ardışık bağlamsız sorgu üçünde de
`NULL` / 0 satır / hatasız geçti.

> ⚠️ Bu maddenin ilk hâli "bağlantının **ikinci ve sonraki kullanımlarında** hata
> fırlatır" diyordu. **Yanlıştı** ve 3. tur denetiminde ölçümle çürütüldü; tetikleyici
> kullanım sayısı değil ilk yazmadır. Metin düzeltildi.

**Sonucu:** çıplak `current_setting('app.tenant_id', true)::uuid` GUC'a hiç
yazılmamış bağlantıda `NULL` → 0 satır verir, **yazılmış** bir bağlantıda
`ERROR: invalid input syntax for type uuid: ""` fırlatır. `pgxpool` altında
unutulan bir `SET LOCAL` bu yüzden **bağlantı geçmişine göre iki farklı** davranır
— taze bağlantıya karşı yazılmış test geçer, üretim patlar.

**Gerekçe:** güvenlik açığı değildi (iki yol da fail-closed, sızıntı yok) ama
belirsizdi ve belirsizlik testi yalancı yapar. `NULLIF` her iki durumda `NULL`
üretir; uçtan uca ölçüldü (aynı bağlantıda tenant A → 1 satır, commit sonrası
bağlamsız → 0, tenant B → 1, hata yok).

**Bedeli açıkça:** `NULLIF` unutulan `SET LOCAL`'i **sessizce** 0 satıra çevirir —
çıplak biçimin gürültülü hatası bir bug'ı daha çabuk yakalardı. Telafi
[M1-07](m1-veri-katmani.md)'ye yazıldı: tenant kapsamlı transaction sarmalayıcısı
`SET LOCAL`'i **kendisi** kurar ve bağlam kurulmadan sorgu çalıştırmayı **API
olarak imkânsız** kılar. Yani koruma politikadan değil giriş noktasından gelir.

**Etkilenen:** CLAUDE.md §6 (çıplak biçimi tek doğru gibi yazıyordu — güncellendi) ·
[M1-01](m1-veri-katmani.md) ADR 0002 · [M1-02](m1-veri-katmani.md)…M1-06 her
politika · [M1-07](m1-veri-katmani.md) sarmalayıcı · [M1-09](m1-veri-katmani.md)
vaka 3 (artık koşulsuz "0 satır, hata değil").

### Q25 — Üç araç düzeltmesi M0-07'ye, dördüncüsü M1-03'e (2026-07-24)

**Karar:** (a), (b), (d) **evet**, [M0-07](m0-bootstrap.md) kapsamında. (c) Q07'ye
bağlı olduğu için [M1-03](m1-veri-katmani.md)'e bırakıldı.

- **(a)** `Makefile:seed` yerel `psql` gerektiriyordu → `docker compose exec -T db psql`
  ile yazılır. Dış bağımlılık kalkar, CI'da da çalışır.
- **(b)** `govulncheck@latest` **pinlenir**. Gerekçe: pinsiz hâlde kod değişmeden
  tarama sonucu değişebilir ve CI bir sabah kendiliğinden kırmızıya döner; sürüm
  yükseltmesi bilinçli bir commit olmalı.
- **(d)** `scripts/redline-check.sh` R5 genişletilir: RLS politikasının yanında
  `tenant_id NOT NULL`, `tenant_id` indeksi ve GRANT de taranır. Böylece CLAUDE.md
  §6'nın "beşinden biri eksikse migration eksiktir" kuralı **mekanik** hale gelir —
  M1'in her migration'ında işe yarar.
- **(c)** `sqlc.yaml`'a `inet[]`/`cidr[]` override'ı: Q07 (`locations.static_ips`
  tipi) kararlanmadan yazılamaz. M1-03'te Q07 ile birlikte. Eklenirse
  [M0-04](m0-bootstrap.md)'ün override tablosu da güncellenmek zorundadır
  ("sabit olan sayı değil, listedir").


### Q26 — Toolchain yükseltildi, mimari arm64'e geçiyor (2026-07-24)

**Karar:** Go **1.26.5** kuruldu (kullanıcı eylemi, oturum dışında yapıldı) ve
yerel toolchain **native arm64**'e geçirilecek.
**Ölçüm:** `govulncheck ./...` artık **"No vulnerabilities found"** — dört stdlib
açığının dördü de kapandı. [M0-07](m0-bootstrap.md)'nin **Bulgu 2'si kendiliğinden
düştü**; kartta kalan tek iş **Bulgu 1** (staticcheck SA1019 → `middleware.RealIP`
router'dan çıkarılır).
**Mimari:** yükseltme Rosetta'yı kaldırmadı — `go version` → `darwin/amd64`, makine
`arm64`, `.tools/tailwindcss` ise arm64 native (repo karma mimari). Karar: **arm64
Go'ya geçilecek**, M0-07 kapsamında. Doğruluğu etkilemiyordu (üretim ikilisi zaten
`linux/amd64` çapraz derlenecek), kazanç yerel derleme/test hızı. Geçişte build
cache ve pinli CLI önbellekleri (`templ`, `sqlc`, `goose`) bir kez baştan derlenir —
ilk `make gen` uzun sürer, bozukluk değil.
**Kalan:** Q25 (b) — `govulncheck@latest` hâlâ pinsiz; tarama sonucu kod
değişmeden değişebilir, CI bir sabah kendiliğinden kırmızıya dönebilir.

### Q04 — DB testleri yerel Postgres'e karşı koşar (2026-07-24)

**Karar:** (a) — testler `make up`'ın ayağa kaldırdığı Postgres'e bağlanır.
CI'da da **`make up` (compose)** koşar, `services: postgres:17` bloğu **değil**.

> **Düzeltme (2026-07-24, M0-06 uygulaması sırasında).** Bu satır önce
> "CI'da `services: postgres:17` + `scripts/db-init/*.sql` uygulanır" diyordu;
> **infeasible.** GitHub Actions `services:` konteynerleri checkout'tan **önce**
> başlar, dolayısıyla repo'daki `scripts/db-init/01-roles.sql`'i (→ `tappa_app`
> rolü, `pgcrypto`/`citext`) hiç uygulayamaz — RLS testinin dayandığı rol ayrımı
> hiç kurulmazdı. CI bu yüzden checkout'tan **sonra** `make up` koşar; init SQL'i
> Postgres entrypoint uygular ve yerel dev'le birebir olur. Kararın **özü
> değişmedi** ("yerel Postgres, testcontainers değil"); yalnız CI'da nasıl ayağa
> kalktığı düzeltildi. Sevk edilen iş akışı: [.github/workflows/ci.yml](../../.github/workflows/ci.yml).
**Gerekçe:** `testcontainers-go` yeni ve ağır bir bağımlılık zinciri getirirdi
(docker API istemcisi + testify) — CLAUDE.md §1 "stdlib > küçük kütüphane >
framework". RLS izolasyonunu test etmek için tek kullanımlık konteyner gerekmiyor;
gereken şey **gerçek** Postgres ve **gerçek** rol ayrımı, ikisi de `make up` ile var.
**Bedeli açıkça:** testler paylaşılan bir DB'ye yazar → [M1-09](m1-veri-katmani.md)
her testin kendi tenant'ıyla çalışmasını ve temizlemesini **kendisi** garanti etmek
zorunda. `make up` unutulursa DB testleri patlar; hata mesajı bunu açıkça söylemeli
("Postgres'e bağlanılamadı — `make up` çalıştırdın mı?"), sessizce **atlanmamalı**.
**Etkilenen kartlar:** [M0-06](m0-bootstrap.md) (CI'da Postgres servisi),
[M1-09](m1-veri-katmani.md).

### Q01 — `tenants.timezone` + lokasyon override'ı (2026-07-24)

**Karar:** `tenants.timezone text NOT NULL DEFAULT 'Europe/Malta'`; bir lokasyon
farklı zaman dilimindeyse `locations.timezone` (nullable) **ezer**. Çözüm sırası:
lokasyon → tenant.
**Gerekçe:** vardiya `10:00–22:00` bir **duvar saati**dir, mutlak an değil; hangi
duvarda olduğu yazılı olmazsa geç kalma hesabı anlamsızdır. Lokasyon override'ı
şimdi bir sütun, sonra bir migration — aynı firmanın farklı ülkedeki şubesi
MVP'de yok ama alan bugün eklenirse bedeli sıfır.
**Değişmeyen:** DB'de her şey `timestamptz` **UTC** (CLAUDE.md §6). Zaman dilimi
yalnız **render** ve **vardiya çözümü** için kullanılır; `transactions.occurred_at`
asla yerel saatte saklanmaz.
**Dikkat:** vardiya çözümünde kullanılacak zaman dilimi, çalışanın profilindeki
lokasyonun değil **tap edilen** lokasyonun zaman dilimidir — CLAUDE.md §5 ve
[Q17](#q17--çapraz-lokasyonda-tap-edilen-lokasyonun-vardiyası-2026-07-24) ile aynı
mantık.
**Etkilenen kartlar:** [M1-02](m1-veri-katmani.md) (tenants),
[M1-03](m1-veri-katmani.md) (locations), [M4-05](m4-tap-motoru.md) (vardiya ve
geç kalma).

### Q21 — A1 azaltması politikaya bırakıldı (2026-07-24)

**Karar:** `tap:ctrGap` bağlam anahtarı + `base:ctr-gap-review` baseline
politikası, **kaynak kapsamlı**. Yoğun şubede (KF St Julians) tenant kapatır,
düşük trafikli yerde (Rusty Bar, HQ, hafta sonu) açık kalır.
**Gerekçe:** URL biriktirme zaten yalnız düşük trafikte işe yarıyor — biriktirilen
URL'ler başkasının tap'iyle ölüyor. Her yerde sıkı eşik, plakete dokunup basmayan
meşru kullanıcıyı da flag'lerdi.
**Dürüst kalan taraf:** bu **çözüm değil, sinyal**. A1 ADR 0005'te kabul edilen
risk olarak yazılı. Eski "> 1000 → flag" eşiği kaldırıldı (biriktirme ~10'luk
boşluk üretir).

### Q22 — M3 v1 kapsamı daraltıldı (2026-07-24)

**Karar:** Motor mimarisi korunuyor; v1'de guardrail + baseline + **form** UI.
Ham JSON editörü → [M9-07](m9-sonrasi.md), simülatör → [M9-06](m9-sonrasi.md).
**Gerekçe:** Dış denetim haklı — iki müşteri için tam yüzey pilotu geciktirir ve
M4'ü genel amaçlı bir motorun doğruluğuna rehin eder. Daraltma pilotu ~8-10 görev
öne çekiyor, mimari kaybı yok.
**Kritik şart:** `policy_context jsonb` (M3-07) **1. günden** yazılır — simülatör
sonra gelse de geçmiş yeniden üretilemez.

### Q23 — Yasal metinler dağıtıldı + pilot kapısı (2026-07-24)

**Karar:** Aydınlatma metni → [M5-02](m5-tap-akisi.md); gizlilik/şartlar/imprint
→ [M7-01](m7-portal.md); DPA + saklama → [M8-02](m8-deploy-pilot.md). Ayrıca
[M8-06](m8-deploy-pilot.md)'ya **altı maddelik pilot kapısı**: hepsi ✓ olmadan
pilot başlamaz.
**Gerekçe:** "BioTime'ın GDPR yükünden kurtulun" diye satılan ürünün kendi
pilotunda aydınlatma metni olmadan çalışması ana satış argümanını çürütür.

### Q24 — Tahsilat elle, sayım otomatik (2026-07-24)

**Karar:** Otomatik ödeme **MVP dışı** (roadmap'e yazıldı). Yeni görev
[M6-12](m6-dashboard.md): aylık faturalanabilir çalışan sayımı + fatura taslağı
+ founding offer'ın 3. ay uyarısı. Gönderim ve tahsilat elle.
**Gerekçe:** İki müşteri için ödeme sağlayıcısı ağır; ama sayım otomatik olmazsa
çalışan sayısı ay içinde değişince faturalanacak rakam tartışmalı olur.

### Q14 — Çalışanlar mekânın WiFi'ına bağlanacak (2026-07-24)

**Karar:** Aktivasyon akışına "mekânın WiFi'ına bağlan" adımı eklenir; pilotta
IP eşleşme **oranı** ölçülür.
**Sonuç:** [M5-02](m5-tap-akisi.md) davet/aktivasyon akışına adım eklendi ·
[M8-06](m8-deploy-pilot.md) pilotta oran ölçümü · IP kanıtı (50 puan) gerçek
bir yol olarak korunuyor.
**Not:** Misafir WiFi teras/kaldırımı da kapsayabilir — IP tek başına "bina
içinde" demek değil. Trust ağırlıkları pilot ölçümünden sonra gözden geçirilecek.

### Q15 — QR kanalında IP zorunlu (2026-07-24)

**Karar:** QR'da `ok` yalnız **IP eşleşmesiyle** verilir; GPS-only QR → `flag`.
**Gerekçe:** QR statiktir, fotoğraflanır, süresiz geçerlidir ve SUN taşımaz —
hiçbir fiziksel dokunuş kanıtı yok. Tek gerçek kanıt mekânın ağında olmak kalır.
**Sonuç:** `base:qr-requires-ip` baseline politikası ([M3-06](m3-policy-motoru.md)) ·
[M5-08](m5-tap-akisi.md) · CLAUDE.md §5 QR maddesi güncellendi. Tenant isterse
politikayı değiştirebilir.

### Q16 — GPS-only: tenant anahtarı, varsayılan `ok` (2026-07-24)

**Karar:** NFC'de GPS-only tap baseline'da `ok` + trust 50 + `verified via GPS`
notu. Tenant tek anahtarla `flag`'e çekebilir.
**Gerekçe:** Mock location gerçek bir risk ama GPS-only'yi baştan `flag` yapmak,
WiFi'a bağlanmayan çalışanların bulunduğu tenant'ta onay kuyruğunu her gün
şişirir. Kararı müşteriye vermek policy motorunun varlık sebebi.
**Sonuç:** `base:gps-only-allow` (anahtarlanabilir) ([M3-06](m3-policy-motoru.md)) ·
GPS-only oranı [M6-11](m6-dashboard.md) anomali raporunda zorunlu metrik.

### Q17 — Çapraz lokasyonda tap edilen lokasyonun vardiyası (2026-07-24)

**Karar:** Kayıt ve vardiya **tap edilen** lokasyondan çözülür; departman
vardiyası (çalışanın profilinden) varsa yine onu ezer. Tap lokasyonu profil
lokasyonundan farklıysa nota işlenir ve raporda ayrı görünür.
**Gerekçe:** Zincirde şube değişimi normal; çalışan o gün fiilen orada. Profil
lokasyonuna göre hesaplamak St Julians (10:00) personeline Paceville'de (11:00)
her gün haksız "geç kaldı" damgası vururdu.
**Sonuç:** `base:cross-location-note` ([M3-06](m3-policy-motoru.md)) ·
[M4-05](m4-tap-motoru.md) vardiya çözümü · CLAUDE.md §5 güncellendi.

### Q18 — Unutulan çıkışta otomatik kapatma yok (2026-07-24)

**Karar:** Sistem hiçbir çıkış kaydı **üretmez**. Açık kalan kayıtlar raporda
anomali olarak listelenir; müdür manuel çıkış girer (`channel='manual'`,
`entered_by`).
**Gerekçe:** Sistemin uydurduğu saat, mesai kaydını hukuki delil olmaktan
çıkarır. Kayıt her zaman bir insanın beyanına dayanır.
**Sonuç — dikkat:** Açık kayıtlar **saat toplamına girmez**; rapor bunları ayrı
"open / needs action" bölümünde gösterir ve toplamın eksik olduğunu açıkça
söyler. Sessizce 0 saat sayılırsa bordro eksik çıkar.
**Nerede:** [M6-07](m6-dashboard.md) raporlar · [M6-08](m6-dashboard.md) manuel
kayıt · [M6-11](m6-dashboard.md) anomali listesi · [M4-04](m4-tap-motoru.md)
yön tayini (çok eski açık giriş davranışı).

### Q19 — Buddy punching: kabul edilen risk + tespit (2026-07-24)

**Karar:** Ürün buddy punching'i **önlemiyor** ve bunu yazılı olarak kabul
ediyoruz. ADR 0005 yazılacak ([M3-09](m3-policy-motoru.md)), satış cevabı
hazırlanacak, [M6-11](m6-dashboard.md)'e tespit sinyali eklenecek.
**Satış cevabının çekirdeği:** 1 oturum = 1 çalışan (bir telefon iki kişiyi
basamaz — karta göre daha zor) · plaket kamera görüş alanında · telefonun ekran
kilidi gönülsüz devri engeller · eş-zamanlı tap çiftleri raporlanır.
**Dürüst kalan taraf:** gönüllü iş birliğini hiçbiri durdurmaz. Parmak izi
sisteminin çözdüğü tek şey buydu; karşılığında enrollment, cihaz arızası, ıslak
el ve GDPR yükünden kurtuluyoruz.

### Q20 — Onaylar ayrı `transaction_reviews` tablosunda (2026-07-24)

**Karar:** FLAGGED onay/ret kararı `transactions`'a **yazılmaz**; append-only
`transaction_reviews` tablosuna yazılır, rapor JOIN ile son durumu okur.
**Gerekçe:** `transactions` yalnız tap kaydı kalır — anlamı net, saat toplamı
şişmez, §4.3 immutability doğal olarak korunur.
**Sonuç:** [M1-06](m1-veri-katmani.md) şemaya eklendi · [M6-04](m6-dashboard.md)
onay kuyruğu buna göre yazılacak · **Y3 kapandı**.
