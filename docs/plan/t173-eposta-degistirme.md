# T173 — Panel kimliğinin e-posta değişikliği: kartlar

> **Durum:** kart yazıldı (2026-11-11, dal `m14-emailcard`), **onay bekliyor** — iki bağımsız inceleyici ve
> kullanıcının ADR 0044 §10 kararları (K-E1 … K-E13). Onay gelmeden kod yazılmaz (kullanıcı kararı 2026-11-11:
> önce kart, iki bağımsız onay, sonra kod). Canlı durum yalnız [state.md](state.md)'dedir.
>
> **Mimari karar:** [ADR 0044](../adr/0044-panel-kimliginin-e-posta-degisikligi.md) (Önerildi). Okumadan
> başlama: §5 (tasarım), §5.4 (düz zamanlama), §5.5 (açık linkler), §6 (geçiş), §10 (kararlar). Normatif
> zemin: [ADR 0031](../adr/0031-tek-kimlik-cok-isletme.md) §5.1, §5.2, §5.6, §5.8, §6.1, §8.
>
> **Aşağıdaki kabul kriterleri ADR 0044 §10'un ÖNERİLEN seçeneklerini varsayar.** Kullanıcı bir kararda
> başka seçeneği seçerse kart o maddede yeniden yazılır ve yeniden onaya gider; yapıcı sessizce uyarlamaz.

## Çalışma modu

- Yapıcı (`opus`) + ayrı üçüncü göz; her üç kart §4.5 (3)'e değer → **`tappa-security-auditor` zorunlu**.
  EC-1 agent `tappa-db-migrator`'ın kurallarıyla; EC-2'nin UI'ı ve e-posta şablonları skill `tappa-brand`'la.
- Testler (CLAUDE.md §10): tur içinde yalnız değişen testler `-run` ile + `gofmt`, build, `make gen` temiz,
  `bash scripts/redline-check.sh`; çapraz paket envanterleri her commit'ten önce
  (`go test ./cmd/tappa/ ./cmd/opadmin/ -run 'ConstantTime|Inventory|EveryNamedTest|StoreSurface|TablesLock' -count=1`).
  Tam `-race`, `go test ./...`, mutasyon seti ve `make check` kart başına **bir kez**, commit'ten önce.
- Henüz var olmayan testin adı belgeye yazılmaz (`TestEveryNamedTestExists`); test tarif edilir.
- **Migration numaraları:** **00101** (EC-1, şema + yetki), **00102** (EC-1, üç fonksiyon), **00103** (EC-1,
  süpürmenin genişlemesi). Başka numara gerekirse orkestratörden istenir.
- Dal: orkestratörün açacağı feature dalı; `main` birleştirmesi bir deploy kararıdır. EC-1 + EC-2 aynı
  deploy'da gider (§6: eski binary yeni şemada çalışır, yeni binary eski şemada koşmaz — migration önce).
- Bayrak: **yeni bayrak yok.** Rotalar `mountGated(SurfaceAccountIdentity)` altında (ADR 0044 §5.10).

## Görev listesi

| Kart | İş | Boyut | Bağımlılık |
|---|---|---|---|
| EC-1 | Şema, yetki, üç `identity_*` fonksiyonu, süpürme (00101–00103) + ADR 0031 §5.2 notu | L | ADR 0044 onayı + K-E1 |
| EC-2 | `adminauth` + handler + mektuplar + hesap sayfası + süpürmenin bağlanması | L | EC-1 |
| EC-3 | Runbook, ön/son kontroller, duman, M14-20b kartına ek | S | EC-1, EC-2 |

---

## EC-1 — Şema, yetki ve kapalı liste ekleri

- **Bağımlılık:** ADR 0044 kabul + K-E1 (a) · goose 99 (bugün son `00099`; `00100` bu kartın değil)
- **Kırmızı çizgi:** §4.5 (3) (kapalı listeye üç fonksiyon, matrise `UPDATE (email)`) · §6 · §7
- **Araç:** agent `tappa-db-migrator` + güvenlik denetçisi
- **Commit:** `feat(db): let an identity change its sign-in address by mailbox proof`

**Dosyalar.** `db/migrations/00101_*.sql`, `00102_*.sql`, `00103_*.sql` · `internal/db/identity.go` (üç çağrı
+ süpürme) · `internal/db/identitycatalog_test.go`, `identitybelt_test.go`, `identityascii_test.go`,
`privcatalog_test.go`, `m11schema_test.go` (envanterler) · `docs/adr/0031-tek-kimlik-cok-isletme.md` §5.2'ye
tarihli not (kapalı liste + matris eki; kullanıcı onayının tarihiyle).

**Kabul kriterleri.**

*Şema (00101)*
1. `admin_identity_verifications`: `purpose` CHECK'i (adı korunur) `('verify','link','change','revert')`;
   `address citext NULL` ve `(address IS NOT NULL) = (purpose IN ('change','revert'))`; `cancelled_at
   timestamptz NULL`; TTL tavanı amaç başına (`'revert'` K-E3'ün süresi, öbürleri 24 saat). Adres biçimi
   CHECK'le **doğrulanmaz** (ADR 0031:261-265 — `DETAIL` sızıntısı).
2. `admin_identity_events_event_known` + `email_change_requested`, `email_changed`, `email_reverted`.
3. Yetkiler yalnız ADR 0044 §5.2'nin listesi: definer'a `admin_identities UPDATE (email)`, `admin_users
   UPDATE (email)`, doğrulama tablosunda `SELECT (address, cancelled_at)`, `INSERT (address)`, `UPDATE
   (cancelled_at)`. `tappa_app` ve `tappa_resolver` için **sıfır** yeni hücre (katalog testi bunu pinler).
   `admin_users` GRANT'ı kendi `SET LOCAL lock_timeout`'uyla; `admin_users` ALTER edilmez.
4. Ön koşul bloğu (00084'ün deseni): 00099 uygulanmış, tablolar ve `purpose` sütunu var; yoksa sabit mesajla
   durur, hiçbir şey değişmez.
5. **Down:** canlı (süresi dolmamış, harcanmamış, iptal edilmemiş) bir `'revert'` satırı varken **sayıyla
   durur**; yoksa `'change'`/`'revert'` satırlarını siler, sütunları ve CHECK'leri 00084/00079'un biçimine
   **bayt bayt** geri koyar, yetkileri alır (pg_get_constraintdef + ACL md5'i 100 ↔ 101 → Down eşit).

*Fonksiyonlar (00102)* — ADR 0044 §5.3'ün imzaları birebir (dönüş sütunlarıyla); üçü de sahibi
`tappa_identitydefiner`, `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, `REVOKE ALL … FROM
PUBLIC`, `EXECUTE` yalnız `tappa_app`, dinamik SQL yok.
6. `identity_request_email_change`: oturum canlı, 12 saatlik yüklem (00085), bağlı ve reddedilmemiş satır;
   kimlik kilidi altında `password_hash = p_verified_digest` değilse 0 satır; 6b yeni adreste (`IF (…) IS NOT
   TRUE THEN RETURN`); K-E9 (a)'nın üç ön koşulu (doğrulanmış adres, pencere kapalı, eski adreste bağsız aktif
   satır yok — açık `tenant_id`'li adlı e-posta-geniş okuma); citext'te kendi adresi → 0 satır. Önceki canlı
   `'change'` iptal, yeni satır, olay. Tenant tablosuna yazmaz, `audit_log`'a yazmaz.
7. `identity_change_email`: ADR 0044 §5.3'ün kilit sırası (adres kilidi → kimlik → üyelikler → link → oturum)
   ve 1–8. adımları. Aynanın güncellemesi kimliğin `unlinked_at IS NULL` **her** bağlı satırına (devre dışı
   dahil), her ifadede `a.tenant_id = m.tenant_id`. `email_verified_at = now()`, onay penceresi **açılmaz**.
   Oturumlar K-E6 (a). Açık linkler ADR 0044 §5.5'in tablosu birebir. Her bağlı işletmeye bir
   `admin.email.changed` satırı: `{"outcome":"ok"}` + yalnız sıfır değilse `revoked_other_sessions`; adres,
   link id'si, `identity_id`, karşı işletme, sayı **yok**. `23505` gövdede yakalanır → 0 satır.
8. `identity_revert_email_change`: ADR 0044 §5.3'ün gövdesi; nöbetçi digest çözmeninkiyle aynı tür (SQL'de
   pgcrypto ya da Go'da — yapıcı seçer, gerekçeler, imza ona göre pinlenir); bağlı satırlara digest kopyası
   (contract B'ye dek); **bütün** oturumlar; kimliğin canlı sıfırlama, `'change'` ve öbür `'revert'` linkleri
   iptal; her işletmeye `admin.email.reverted` (aynı biçim); eski adres başka bir kimliğinse 0 satır.
9. Kimlik başına en çok **bir** canlı `'change'` ve **bir** canlı `'revert'` — kilit altında; zincirde (A→B→C)
   ikinci değişim `'revert'` yazmaz.

*Süpürme (00103)*
10. `identity_sweep_events()` **aynı imzayla** (`CREATE OR REPLACE`; sahip, `SECURITY DEFINER`, `search_path`,
    `REVOKE`/`GRANT` yeniden yazılır — T5) ve ek olarak harcanmış / iptal edilmiş / süresi dolmuş
    `'change'`/`'revert'` satırlarını 1 gün sonra siler; `'verify'`/`'link'` satırlarına dokunmaz. Definer'a
    doğrulama tablosunda `DELETE`. Down 00072'nin gövdesini bayt bayt geri koyar.

*Katalog ve kemer*
11. İmza tablosu, fiil × sütun matrisi, "her yetki kullanılıyor" egzersizi, `privCatalogAllowed`,
    `m11Functions`, operatör rollerinin yürütemediği fonksiyonlar listesi aynı commit'te genişler.
12. Kimlik kemeri üç gövdeyi adıyla tarar; aile başına kontrol sayısı (T178 deseni, `identityBeltFamilies`);
    adres kuralı envanteri (`TestIdentityAddressRule_EveryIdentityInsertDecidesIt`) `admin_identities`'e
    `UPDATE … SET email` yazan her fonksiyonu da aday sayacak biçimde genişler — yeni iki yazarın reddi
    gerçek bir `IF (…) IS NOT TRUE` olmalı.
13. ADR 0031 §5.2'ye tarihli not (kapalı liste + matris eki, kullanıcı onayının tarihiyle). §6.1 ve CLAUDE.md
    §4.5 (3) **değişmez** (K-E1 (a)); `TestClaudeMD_TheIdentityExceptionIsADR0031sSection61` yeşil kalır.

**Testler (gerçek Postgres; adlar yapıcının).**
- Her ret dalı: 0 satır **ve** hiçbir tabloda yazı yok (öncesi/sonrası sayım) — bilinmeyen, iptal, harcanmış,
  süresi dolmuş link; başka kimliğin oturumu; 12 saati geçmiş oturum; digest uyuşmazlığı; doğrulanmamış adres;
  açık pencere; eski adreste bağsız satır; yeni adres bir kimliğin; kişinin bir işletmesinde aynı adresli satır;
  6b'den geçmeyen adres (U+0130, U+212A).
- 1, 2 ve 10 işletmeli kimlik: değişimden sonra her bağlı satırda ayna = kimlik adresi; devre dışı üyelik de;
  reddedilmiş satır eski adreste.
- Tetikleyici: değişimden sonra `tappa_app` ve `tappa_owner` bağlı satırın `email`'ini yazamaz (00069'un
  birinci kolu).
- Oturumlar: onaylayan kalır, öbürleri her işletmede kapanır; geri almada hepsi.
- Açık linkler: kimlik sıfırlaması ve bağlı satırların satır başı linkleri iptal ve tüketimleri 0 satır;
  `'verify'`/`'link'` linkleri dokunulmamış; eski adrese davet kabulü 0 satır, yeni adrese davet kabulü çalışır.
- Zincir: A→B→C, iki eski kutu → yalnız A'nın linki geri alır; geri almadan sonra her link iptal.
- Eşzamanlılık (`-race`, N goroutine): aynı link iki onay → tam bir başarı; aynı yeni adrese değişim + kayıt
  sahiplenmesi + davet kabulü → tam biri o adresin kimliği olur, 500 yok, hata metninde adres yok;
  değişim + parola sıfırlama tüketimi → kilitlenme yok.
- Audit biçimi: satır anahtarları tam `outcome` (+ sıfır değilse sayı); hiçbir audit `detail`'inde adres, hiçbir
  işletmede karşı işletmenin id'si.
- RLS izolasyonu: tenant A'nın bağlantısı (filtresiz sonda) değişimden sonra B'deki satırı görmez.
- Down: 100 ↔ 101 ↔ 102 ↔ 103 gidiş-dönüş; canlı geri alma linkiyle Down durur.

**Mutasyon kanıtları (her biri: kaldır → kırmızı → geri al; raporda listelenir).**
oturumun 12 saat yüklemi · digest yeniden denetimi · 6b reddi (iki yazarda ayrı) · "yeni adreste kimlik yok"
yeniden denetimi · aynı işletmede aynı adresli satır denetimi · aynanın `tenant_id` yüklemi · aynanın
`identity_id` yüklemi · `23505` yakalaması · kimlik sıfırlamalarının iptali · satır başı linklerin iptali · "tek
canlı `'revert'`" kuralı · geri almada "eski adreste kimlik yok" · K-E9'un üç ön koşulu (ayrı ayrı) · audit
`detail`'inin sıfırda sayıyı düşürmesi · süpürmenin `'verify'`/`'link'`'e dokunmaması.

---

## EC-2 — Uygulama: istek, onay, bildirim, geri alma

- **Bağımlılık:** EC-1
- **Kırmızı çizgi:** §4.5 (3) · §4.6 · §7 · §9 (hesap sayfası; tap/sonuç ekranına dokunulmaz)
- **Araç:** yapıcı (skill `tappa-brand`) + güvenlik denetçisi
- **Commit:** `feat(panel): change the sign-in email from the account page`

**Dosyalar.** `internal/adminauth/` (yeni dosya; `identityaccount.go`'nun deseni: iki yeni türetilmiş HMAC
anahtarı — değişim ve geri alma, `deriveVerifyKey`'in biçimi) · `internal/handler/` (hesap sayfası formu,
istek, link inişi, onay, geri alma inişi + onayı; ayrılmış mektup kuyruğu) · `internal/handler/adminratelimit.go`
(yeni sabitler, `TestPanelConstants_ShippedValuesArePinned`'e) · `web/templates/` (hesap kartı, onay ve geri alma
sayfaları, dört mektup) · `cmd/tappa/main.go` (süpürmenin periyodik çağrısı — T294).

**Kabul kriterleri.**

*İstek*
1. Hesap sayfasında, yalnız bağlı kimlik oturumunda, **"Change your sign-in email"** kartı (K-E10): yeni adres +
   mevcut parola. Bağlanmamış satırın oturumu kartı görmez ve rotası `unavailable` döner (ücretsiz — kişinin
   kovasını harcamaz).
2. Sıra: iş kapısı → kişinin kovası (`TryCharge`, karşılaştırmadan önce) → adres biçimi (`mail.ValidRecipient`,
   kopya değil) → mevcut parola (`identity_password_for_session`'ın digest'i, `changepassword.go`'nun deseni) →
   `resolve_identity_by_email(yeni)` (yalnız mektubu seçmek için) → `identity_request_email_change` →
   mektubun kuyruğa bloklamadan sunulması → **taban** → 303 + aynı flash.
3. Yanlış parola: görünür ret (*"Your current password is not right"*) — kişinin kendi bilgisi; kova harcanır.
   K-E9'un ön koşulları ve K-E13: görünür, sabit cümleler (K-E10).
4. **Düz zamanlama:** çakışmasız / yeni adres bir kimliğin / yeni adres kimsesiz ama kişinin bir işletmesinde
   satırı var — üç kol aynı yanıtı (gövde baytları, durum, başlıklar; oturuma özgü değerler hariç) ve aynı
   tabanı öder; HTTP üzerinden gerçek Postgres'le iç içe ölçüm, medyanlar aynı bantta (biçim:
   `TestPanelIdentityReset_TimingIsFlatOverHTTP`). Taban her çıkışta uygulanır (yapısal test).
5. Mektup kuyruğu sıfırlama outbox'ından **ayrıdır** (sınırlı, süreç içi; dolunca o mektup gönderilmez ve bir
   log satırı yazılır, yanıt değişmez); işçi panikleri yakalar, panik değerini log'a yazmaz (ADR 0022 §6.4'ün
   deseni). Çakışmasız kolda değişim linki, çakışma kolunda linksiz bilgi mektubu (K-E5); ikisi de kesiciden
   `Send` ile, alıcı tavanı kişinin kapsamıyla.

*Onay*
6. Link GET'te harcanmaz: değer kısa ömürlü, dar yollu bir çereze girer ve tarayıcı hesap sayfasına gider
   (`verifyLinkLanding`'in deseni); oturum yoksa giriş önce, çerez bekler. Sayfa onay düğmesini adres
   **anmadan** çizer (K-E10; çözümleyici eklenmez).
7. Onay POST'u `identity_change_email`'i link hash'i + çerezin oturum hash'i + yeni geri alma linkinin hash'iyle
   çağırır; ret tek sayfa (*"This link can't be used…"*), dalları ayırt etmez.
8. Başarıda: eski adrese **bir** bildirim (passwordnotice deseni — istek içinde, outbox'tan değil,
   `SendExempt`, kendi gönderim süresi sınırı, kimlik başına saatte 5); K-E2 (a) ise mektup geri alma linkini
   taşır, K-E8 (a) yeni adresi anmaz. Yeni adrese *"your sign-in email is now this address"* onayı (K-E10).
   Bildirim başarısız olursa değişiklik durur ve yanıt yine başarıdır; sonuç bütçeli bir log satırıdır (audit
   değil — kimliğin bildirimi hiçbir işletmenin değildir).

*Geri alma*
9. Geri alma linki GET'te harcanmaz (aynı desen, kendi çerezi); oturum **istemez**; onay sayfası ne olacağını
   söyler (eski adres geri gelir, her yerde çıkış, yeni parola linki). POST'ta IP kovası
   (`adminResetSubmitLimit`'in deseni) → `identity_revert_email_change` → başarıda eski adrese
   `identity_open_reset` ile bir sıfırlama linki (K-E11 (a); alıcı `identityRecipient` bağıyla, M14-14'ün yolu)
   → sabit bir sonuç sayfası. Ret tek sayfa.

*Güvenlik ve veri*
10. Token'lar yalnız mektuba girer; hiçbir log, yanıt, hata metni ya da veritabanı satırında değil (veritabanı
    hash'i tutar). İki yeni HMAC anahtarı birbirinin ve doğrulama/link anahtarlarının hash'ine çözülmez (test).
11. Log anahtarları ADR 0022 §10 + `identity_id`; adres hiçbir satırda (her harf büyüklüğünde), panik değeri yok;
    röle davranışlarının hepsinde (biçim: `TestPasswordNotice_LogsOnlyIdsClassAndCode`).
12. Mektuplar ADR 0022 §4 (sabit ASCII konu, alıcı kuralı) ve §8 şablon kurallarıyla; tek link
    `TAPPA_BASE_URL`'den, istek başlığından asla (resetmail.go'nun kuralı; düşman `Host` /
    `X-Forwarded-Host` ile test).
13. Her POST panelin CSRF/Origin + `tb` bağıyla (M14-10a); geri alma ve link inişleri sıfırlama akışının
    bağlamasıyla.
14. Süpürme `cmd/tappa/main.go`'da taslak süpürmesinin yanına bağlanır (sabit periyot); çağrıldığı bir testle
    pinlenir (T294'ü kapatır).

*Doğrulama*
15. Her yeni korumanın mutasyon kanıtı: taban · kova önce · bağlanmamış satırın ücretsiz reddi · kuyruğun
    ayrılığı (sıfırlama outbox'ı doluyken mektup yine kuyruğa girer) · GET'in harcamaması (iki iniş) · oturum
    şartı (onay) · `SendExempt` (kesici açıkken bildirim gider) · HMAC anahtar ayrılığı · log anahtar kümesi.
16. Gerçekçilik (kullanıcı kuralı "piyasa deseni önce"): raporda her görünür adımın ana akım karşılığı
    (Google/Microsoft/GitHub: mevcut parola, yeni adrese doğrulama, eski adrese "bu sen değilsen" bildirimi).

---

## EC-3 — Canlıya alma

- **Bağımlılık:** EC-1, EC-2 · **Araç:** yapıcı + üçüncü göz (güvenlik denetçisi EC-1/EC-2'ninkini kapsar)
- **Commit:** `docs(deploy): runbook for the sign-in email change`

**Kabul kriterleri.**
1. `deploy/README.md`'ye bölüm: ön kontroller (salt okuma: goose 99, `admin_identity_verifications`'ta yalnız
   `'verify'`/`'link'`, adresi kimliğinkinden farklı bağlı satır = 0), deploy sonrası kontroller (goose 103,
   üç fonksiyonun sahibi/ACL'i/`search_path`'i, `tappa_app`'ın `admin_users.email`'e hâlâ yazamadığı,
   `identity_sweep_events`'in yeni gövdesi) ve duman (iki işletmeli kimlik: değiştir → iki işletmede ayna ve
   audit → eski adrese bildirim → geri al → yeni parola linki → giriş).
2. Geri dönüş paragrafı: önceki imaj; Down ancak sonraki deploy'da ve canlı geri alma linki yokken (ADR 0044 §6).
3. `docs/plan/m14-tek-kimlik.md` M14-20b'nin 1b listesine `identity_revert_email_change` (digest kopyalar →
   contract B'den sonra `NULL`) ve ADR 0031 §8 adım 8'e aynı satır — kullanıcı onaylı ADR 0044'e atıfla.
4. Haftalık sayımlar bölümüne (varsa) bir satır: canlı geri alma linki sayısı (salt okuma, adres basmadan).
