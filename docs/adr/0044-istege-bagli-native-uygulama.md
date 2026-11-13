# ADR 0044 — İsteğe bağlı native personel uygulaması ve NFC'siz telefonlar

- **Durum:** Önerildi (2026-11-13). Kabul M16-01'de, kullanıcı onayıyla; kabul edilmeden hiçbir M16 kod kartı başlamaz.
- **Tarih:** 2026-11-13 · **Dal:** `plan/m16-mobil` (yalnız belge) · **Plan:** [docs/plan/m16-mobil.md](../plan/m16-mobil.md)
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-12/13. Kullanıcının **açık** kararları yalnız beş tanedir ve bu
  belge onları kaydeder, yeniden tartışmaz: (1) native uygulama **isteğe bağlı**, tarayıcı akışı kalır; (2) **tamamen
  native** — Swift/SwiftUI, Kotlin/Compose, Node içeren yol (RN/Expo) dışarıda; (3) NFC'siz telefon için **statik ve
  canlı QR birlikte**; (4) NFC'siz aktivasyon **canlı QR varsa onunla, yoksa müdür onayıyla**; (5) **attestation Faz E'de
  yalnız rapor**. Geri kalan her şey — mekanizmalar, adlar, yerler, retler, faz sırası — **plan önerisidir**:
  *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)* Bölümlerde *"karar"* ya da *"plan önerisi"* diye ayrıca işaretlidir.
- **İlgili:** ADR 0001 (stack) · ADR 0004 (policy motoru; §8 *kanal sunucudan türetilir*) · ADR 0005 (kabul edilen
  riskler; risk 1 / A4 buddy punching) · ADR 0006 (debounce sunucu zamanı) · ADR 0017 (Android encode rölesi — repodaki
  ilk Kotlin/Gradle açılımı) · ADR 0026 (aktivasyon ilk NFC dokunuşunda; **Bedel 3**) · ADR 0032 (iş günü, tahmini yön) ·
  ADR 0038 (`transactions` INSERT sütun listesi) · CLAUDE.md §1, §4.1–§4.7, §5, §9 · open-questions A1–A4, P6, Q11, Q15, Q16.
- **Atıf kuralı:** `file:line` atıfları `origin/main` `70e8a016`'ya göredir; **Okundu** = dosyadan okundu, çalıştırılmadı.
  **Doğrula** = bu belgeyi yazan doğrulayamadı; ilgili kart ölçer ve sonucu buraya not olarak yazar.

---

## 1. Bağlam — neden şimdi

Bugünkü akışın dört ölçülmüş ya da kayıtlı boşluğu var; dördü de tarayıcının yapamadığı bir şeyden doğuyor:

1. **iPhone 7–X gerçek NFC kanıtı veremiyor (P6).** Bu telefonlar plaketi arka planda okumaz; çalışan kalıcı olarak
   statik QR'a düşer, `base:qr-requires-ip` gereği mekânın Wi-Fi'ına bağlı değilse **her gün `flag`** üretir
   (open-questions P6, M5-08 kartı). Bu telefonlar Core NFC ile **ön planda** NDEF okuyabilir — ama yalnız bir
   uygulama içinden.
2. **NFC'siz telefon aktive olamıyor (ADR 0026 Bedel 3).** Aktivasyon yalnız gerçek NFC dokunuşunda tamamlanır;
   QR fiziksel dokunuş kanıtı taşımadığı için bilinçli olarak dışarıda. ADR 0026 çözümü *"ayrı bir ürün kararı"* diye
   bıraktı (`docs/adr/0026-aktivasyon-ilk-nfc-dokunusunda.md:92-94`).
3. **"Kim" kanıtı bir taşıyıcı (bearer) çereze dayanıyor.** `tappa_session` (`internal/session/cookie.go:33`,
   `Max-Age` ~1 yıl, `:43`) kopyalanabilir; iOS Safari ITP altında ömrü **ölçülmedi** (Q11, açık).
4. **Web sahte konumu göremiyor (A3).** GPS-only `ok` bir beyandır; Q16 bunu bilerek kabul etti.

Uygulama bu dördüne de dokunur; ama ürünün vaadi *"uygulama kurulumu yok"*tur (CLAUDE.md giriş paragrafı,
handoff §1). Kullanıcının kararı bu gerilimi **isteğe bağlılıkla** çözer.

## 2. Karar

### 2.1 Uygulama isteğe bağlıdır ve eklemelidir

- **Karar (kullanıcı):** native uygulama isteğe bağlıdır; tarayıcı akışı **değişmez ve tam desteklenir**. Uygulamasız
  bir çalışan bugünkü her şeyi yapar.
- Bu alt bölümün aşağıdaki **üç maddesinin üçü de plan önerisidir** *(kullanıcı 2026-11-12'de planı gördü ve
  yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir)*:
- **(Plan önerisi) Aynı plaket, aynı URL.** Plaketin NDEF kaydı `TAPPA_BASE_URL` + `/t` taşır (`internal/handler/tap.go:282`,
  `PlaqueTapBase` `:290`; üretimde `https://taptime.mt` — `deploy/k8s/43-redirect-ingress.yaml:7`). Uygulama kuruluysa
  bu URL iOS Universal Link / Android App Link ile uygulamayı açar; değilse tarayıcıyı. Plaket yeniden kodlanmaz.
- **(Plan önerisi) Sunucu otoriterdir.** Aynı `tap.Decide` + policy motoru karar verir; uygulama ek kanıt taşıyan ince bir
  istemcidir, karar vermez, SUN doğrulamaz, yön hesaplamaz.
- **(Plan önerisi) Çalışan tap ekranı kuralları uygulamada da geçerlidir** (CLAUDE.md §9): tek ekran, tek düğme (M15'in sayılı
  *"Not right?"* istisnasıyla), onay ekranında düğme yok, tenant markasının sayılı slotları. Uygulama ekranları tarayıcı
  ekranlarının **eşidir**; bir farklılık §9 sorusudur.

### 2.2 Stack: tamamen native

- **Karar (kullanıcı):** iOS **Swift + SwiftUI**, Android **Kotlin + Jetpack Compose**; React Native / Expo dışarıda —
  Node ister, CLAUDE.md §1 Node'u yasaklar.
- Bu alt bölümün geri kalanı **plan önerisidir** *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)*
- **Reddedilenler (öneri):** Flutter / Kotlin Multiplatform —
  NFC, App Attest ve Play Integrity yine native köprü ister, uygulama küçüktür; ortak katmanın kazancı ikinci bir
  toolchain'in bedelini karşılamaz.
- **Yer:** bu repoda `mobile/ios/` ve `mobile/android/`. İkisi de Go modülünün **dışında**dır (`go build ./...`
  görmez). Repodaki mevcut `android/` dizini **başka bir uygulamadır** — ADR 0017'nin plaket encode rölesi
  (`android/README.md`, paket `mt.taptime.relay`) — ve yerinde kalır; taşımak M16'nın parçası değildir.
- **Üçüncü parti SDK yok:** Firebase, analitik, çökme raporlama SDK'sı yok. Her bağımlılık CLAUDE.md §1 gereği önce
  sorulur; aday sorular open-questions **Q36** (mobil) ve **Q37** (Go tarafı).
- Kotlin/Gradle zinciri repoda ADR 0017 ile **tek bir dizin için** açılmıştı (`android/settings.gradle.kts` başlık
  yorumu). Bu ADR onu `mobile/android/`'e, Swift/Xcode zincirini `mobile/ios/`'a genişletir; kabulü CLAUDE.md §1'e
  bir satır olarak girer (M16-01).

### 2.3 Kimlik: dışa aktarılamayan cihaz anahtarı, taşıyıcı token yok

Bu alt bölüm **plan önerisidir** *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)*

- Uygulama aktivasyonda bir **P-256 anahtar çifti** üretir: iOS Secure Enclave, Android Keystore (StrongBox varsa
  orada). Özel anahtar **cihazdan çıkamaz**; sunucu yalnız açık anahtarı saklar (`app_devices`, §6 beşlisiyle).
- **Uygulamada taşıyıcı token yoktur.** Her durum değiştiren istek, sunucunun verdiği **tek kullanımlık nonce** dahil
  bir kanonik metin üzerinden imzalanır. Kopyalanan bir istek ya da çalınan bir dosya ikinci bir istek üretemez.
  (Öneri: kanonik metin = yöntem, yol, gövde özeti, nonce, cihaz kimliği; nonce kısa ömürlü, tek ifadeli atomik
  tüketim — `ctr`'ın disiplini, CLAUDE.md §4.4'ün şekli: `UPDATE … WHERE consumed_at IS NULL … RETURNING`, 0 satır →
  ret.)
- **Anahtar erişim denetimi biyometri içermez** (§4.1, §3'te ayrıntı): iOS'ta `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`
  izinli; `.biometryAny`, `.biometryCurrentSet`, `.userPresence` yasak. Android'de `setUserAuthenticationRequired`
  yasak. Telefonun ekran kilidi bizim katmanımız değildir; kilidi açık telefon anahtarı kullanabilir.
- **Tenant bilinmeden çözümleme.** İmzalı istek geldiğinde tenant henüz bilinmez ve FORCE RLS altında `tappa_app`
  `app_devices`'ı göremez. Cihaz kimliğinin (ve Faz D'de ekran oturumunun, eşleşme kodunun) çözümü `00003`'ün
  `resolve_session_by_token_hash` desenindeki dar bir SECURITY DEFINER fonksiyonudur: sahibi `tappa_resolver`, en çok 1
  satır, sütun düzeyi GRANT, `SET search_path = pg_catalog, pg_temp`. Tenant çözüldükten sonra her şey `SET LOCAL
  app.tenant_id` altında normal sorgudur.
- **Aktivasyon iki adımdır:** *bağla* (davet kodu + onay + açık anahtar; ADR 0026'nın onay POST'u gibi tüketmez, cihazı
  etkinleştirmez) ve *tamamla* (bağlı anahtarla imzalı, ilk gerçek NFC okumasının SUN URL'si): `CompleteByTap`'in sırası —
  aynı tenant'ta aktif plaket → `sun.Verify` (atomik `ctr`, §4.4) → tek ifadeli tüketim. `transactions` satırı yazmaz.
- **Doğrulama ayrı bir pakette** yaşar (öneri: `internal/appdevice`). CLAUDE.md §3'ün `internal/sun` için *"Kriptografi
  SADECE burada"* cümlesi *"plaket kriptografisi"*ye daraltılır (M16-01): cihaz imzası (ECDSA) ve canlı QR token imzası
  plaket kriptografisi değildir, kendi paketlerinde yaşar. `internal/session` dokunulmaz: paket başlığı
  *"no device probing, no attestation"* der (`internal/session/manager.go:18-22`) ve o söz tarayıcı oturumu için
  doğru kalır.
- **Cihaz çözümlemesi oturumun gerçeklerini döndürür:** çalışan, tenant, iptal durumu — `deactivated` bir çalışanın
  cihazı yine çözülür ki §5 satır 4'ün kaydı ve güvenlik uyarısı yazılsın (`internal/session/manager.go:8-16`'nın
  gerekçesi aynen).
- **Kişi başına tek "kim" kimlik bilgisi (öneri, Q40).** ADR 0026'nın *"ikinci cihaz ilkinin yerini alır"* kuralı
  (`internal/handler/activate.go:1068-1077`) uygulamaya genişler: uygulama aktivasyonu tarayıcı oturumlarını, tarayıcı
  aktivasyonu uygulama cihazlarını iptal eder.
- **İptal yolları ilk günden uygulama cihazlarını da kapsar:** çalışan deaktivasyonu ve panelin *"telefonlarını
  çıkar"* eylemi (T59, `internal/handler/employeeactions.go:690`). Handoff §2'nin *"offboarding: tek tık → oturum o
  saniye ölür"* sözü uygulamada da doğru kalmalıdır.
- Bu, Q11'i **uygulama kullanıcıları için** çözer (çerez yok); tarayıcı kullanıcıları için Q11 açık kalır.
- **Buddy punching değişmez** (ADR 0005 risk 1, A4): telefonunu arkadaşına veren çalışan yine geçer. Cihaz anahtarı
  oturumun *kopyalanmasını* durdurur, telefonun *devredilmesini* değil.

### 2.4 Uygulamanın eklediği kanıt

Bu alt bölüm **plan önerisidir** *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)*

- **iOS Core NFC ön plan okuma.** iPhone 7–X'te uygulamanın tek düğmesi bir NDEF okuma oturumu açar; çipin yazdığı
  URL (taze `ctr` + CMAC) olduğu gibi sunucuya gider → `channel='nfc'`, gerçek SUN kanıtı. P6 bu telefonlar için
  kapanır. iPhone XS ve sonrası arka planda okur ve Universal Link uygulamayı açar (**doğrula**: arka plan okumanın
  bildirimi kurulu uygulamayı açıyor mu — gerçek cihazda, M16-17).
- **Sahte konum bildirimi.** Android `Location.isMock()` (API 31+; öncesinde `isFromMockProvider()`) → bağlam anahtarı.
  Öneri: iOS 15+ `CLLocation.sourceInformation.isSimulatedBySoftware` aynı anahtara (**doğrula**, M16-17).
  Bu bir **istemci beyanıdır** (GPS'in kendisi gibi): *pozitif* bir bildirim kuralı sıkılaştırır; bildirimin
  **yokluğu hiçbir şey kanıtlamaz**.
- **Yeni baseline (öneri adı):** `base:mock-location-review` — sahte konum bildirildi → `review` (`flag`), tenant
  değiştirebilir. Kullanıcının çalışma adı `base:mock-location-flag`'di; mevcut adlandırma etkiyi `-review` son ekiyle
  söyler (`base:ctr-gap-review`, `base:gps-conflict-review` — `internal/policy/baseline.go:214-215`). Kabulde
  kesinleşir.
- **`transactions.client`** (`browser` | `app`): yeni, yalnız INSERT edilen sütun; değişmezlik (§4.3) bozulmaz,
  tarihsel satırlar NULL kalır, geri doldurma yok. Değer **sunucudan türetilir** — hangi uç noktanın isteği
  kabul ettiği (`/api/checkin` → `browser`, `/app/v1` → `app`); gövde, başlık ya da user-agent okunmaz (ADR 0004 §8'in
  kanal kuralıyla aynı ilke, `internal/domain/tap/types.go:57`).
- **Policy bağlamı:** `Client` ve `MockLocation` `tap.Input`'a ve `policy_context`'e girer; tenant bunlarla kural
  yazabilir. **Güven puanı formülü değişmez** (`20 + 50 + 30`).

### 2.5 NFC'siz telefonlar: statik QR **ve** canlı QR

**Karar (kullanıcı):** statik QR ve canlı QR birlikte desteklenir. Biçimleri (ekran sayfası, ~20 sn token, varsayılan
politika, kanal adı) ve aşağıdaki retler **plan önerisidir** *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)*

- **Statik QR** bugünkü `channel='qr'`dır; `base:qr-requires-ip` varsayılanı değişmez. Tarayıcı kamerasıyla ya da
  uygulamayla okunur.
- **Canlı QR** (`channel='qr_live'`, yeni kanal değeri): Go ikilisinin sunduğu bir **ekran sayfası** — mekândaki
  herhangi bir tablet ya da eski telefonda açılır, **uygulama gerekmez**; panelden bir lokasyona eşlenir; sunucunun
  imzaladığı, kısa ömürlü (~20 sn) dönen bir token gösterir. Lokasyon başına **isteğe bağlı** açılır.
  - İmza anahtarı yalnız sunucudadır; ekran cihazı sır taşımaz, yalnız eşlenmiş bir ekran oturumu taşır (öneri).
    Ekran kişisel veri göstermez (ad yok, liste yok).
  - **Varsayılan politika:** `qr_live` + (IP **veya** GPS) → `ok`; ikisi de yok → `flag`. **Okundu:** bu sonuç mevcut
    baseline'dan zaten çıkar — `base:ip-or-gps-ok` ve `base:gps-only-allow` kanal koşulu taşımaz, `base:qr-requires-ip`
    yalnız `channel == "qr"`'de eşleşir (`StringEquals`, `internal/policy/baseline.go:307-309`), `base:no-evidence-review`
    geriye kalanı `review`'a çeker. Yani canlı QR için **yeni baseline belgesi gerekmeyebilir**; M16-21 bunu
    `TestDecide_` vakalarıyla ölçer.
  - **Kişi başına debounce geçerlidir** (§5 satır 5); sayaç yoktur, debounce bu kanalda da tek frendir — **kanallar
    arası** ve **sunucu saatiyle** (ADR 0006): `SecondsSinceLastRecordedTap` bugün `channel IN ('nfc', 'qr')` ile
    süzer (`db/queries/transactions.sql:317`), `qr_live` oraya girmelidir.
  - **Plaketsiz tap.** Motor her çözülmüş tap'te bir plaket varsayar (`internal/domain/tap/types.go:471-473`); §5 satır 1,
    lokasyon/tenant çözümü (`resolve_tag_by_uid`), holding dokunuşunun site sütunları ve `/v1` `tagId`'si de öyle.
    `qr_live`'da lokasyon ve tenant token'dan, `00004`'ün `resolve_tag_by_uid`'inin eşi olan dar bir SECURITY DEFINER çözümleyiciyle türer (holding'de başka tenant'ın ekran satırını okuyabilmeli; iptal ekran satırındadır, token'da değil); iptal edilmiş ekran satır 1'in eşidir; token tenant'ı oturum
    tenant'ıyla eşleşmelidir (holding istisnası statik QR'ın kuralına göre). Ayrıntı M16-21.
  - **Süre dolmuş / imzası bozuk token'ın sonucu** açık soru (Q39).
  - `/v1` punch akışındaki karşılığı açık soru (Q38).
- **Reddedilenler (plan önerisi):** BLE beacon (donanım ister ve arka plan izleme ayartması §4.2'ye değer) · Wi-Fi SSID/BSSID
  (konum izni ister, IP kanıtı zaten aynı şeyi söyler) · paylaşılan kiosk + PIN (buddy punching'i kolaylaştırır —
  telefon artık kişinin değil mekânındır).

### 2.6 NFC'siz aktivasyon — ADR 0026 Bedel 3 kalkar

**Karar (kullanıcı):** canlı QR ekranı varsa onunla, yoksa müdür onayıyla. Müdür onayı yolu yalnız ekran **yokken**
sunulur (sunucu kapısı, M16-23). Eşleşme kodu ve tablo biçimi plan önerisidir.

- Lokasyonda canlı QR ekranı varsa: onaylı (bağlı) aktivasyonu olan tarayıcı ya da uygulama canlı QR'ı okur →
  aktivasyon tamamlanır. `CompleteByTap`'in sırası korunur, `sun.Verify`'ın yerini canlı token doğrulaması alır.
- Yoksa: **müdür onayı.** Çalışan sihirbazın son adımında *"telefonum plakete dokunamıyor"* yolunu seçer; panelde
  bekleyen cihaz listesi açılır; müdür onaylar; onay `audit_log` yazar. Öneri: telefonda ve panelde aynı kısa
  **eşleşme kodu** — müdür kodu karşısındaki kişinin ekranında görür.
- ADR 0026'nın denetimleri aynen: davet geçerli, onay + bağ tek ifadede tüketilir, plaket/ekran **davetle aynı
  tenant'ta** ve aktif, çalışan aktif. Bu dokunuş da `transactions` satırı yazmaz.
- Faz D yayına çıkınca ADR 0026'ya bir not düşülür: Bedel 3 **iptal edilir ama silinmez** (ADR 0005'in append
  kuralının ruhu).

### 2.7 Attestation — yalnız kanıt ve rapor (Faz E)

**Karar (kullanıcı):** yalnız kanıt + rapor, karar vermez. `policy_context` dışında tutulması ve kayıt biçimi plan
önerisidir.

- App Attest (iOS) / Play Integrity (Android) sonucu **kaydedilir** (öneri: `device_attested`) ve raporda görünür
  (M6-11 tarzı oranlar: uygulama / tarayıcı / attested / sahte konum).
- **Güven puanını ve verdict'i değiştirmez.** Bu yüzden `policy_context`'e **girmez**: girerse tenant onunla kural
  yazabilir ve onu karar verici yapar. Karar verici yapmak ayrı bir ADR'dir.

### 2.8 Çevrimdışı kuyruk — ertelendi

**Plan önerisi** *(Plan önerisi — kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar vermedi; M16-01'de ADR kabulüyle kesinleşir.)* §4.6 ve ADR 0038 (yazılma anı veritabanınındır) gereği: kuyruktan gelen dokunuş sunucu saatini alır, bir gecikme
eşiğinin ötesinde `flag`'lenir (bugünkü `base:queued-window`, 120 sn — `internal/policy/baseline.go:229`). M16'nın
sonrasına bırakıldı (M9-01 ile birlikte düşünülür).

### 2.9 Fazlar (plan önerisi)

0 Karar · A Sunucu temeli · B Android MVP · C iOS MVP (B ve C, A'dan sonra paralel worktree'lerde) · D NFC'siz ·
E Kanıt + rapor · F Yayın + pilot · Sonra (çevrimdışı kuyruk, konumsuz unutulan çıkış bildirimi, *"saatlerim"*
ekranı). Kartlar: [m16-mobil.md](../plan/m16-mobil.md).

## 3. Kırmızı çizgiler — madde madde

| § | Uygulamada ne demek | Mekanik kapı (M16-02) |
|---|---|---|
| **4.1** biyometri | `LocalAuthentication` / `LAContext` yok; `BiometricPrompt` / `androidx.biometric` yok; anahtar erişim denetiminde biyometri ya da kullanıcı varlığı bayrağı yok; Face ID kullanım açıklaması yok | `redline-check.sh` R1 `mobile/`'ı da tarar, platform kalıplarıyla |
| **4.2** GPS yalnız tap anında | iOS: yalnız *When-In-Use* izni, tek seferlik konum; `UIBackgroundModes` içinde `location` yok, *Always* izni yok, bölge izleme yok. Android: `ACCESS_BACKGROUND_LOCATION` yok, `location` tipli ön plan servisi yok, sürekli güncelleme isteği yok, geofence yok | R2 `mobile/`'ı da tarar; Android'de **birleştirilmiş** manifest de denetlenir (kütüphane izni sızdırabilir) |
| **4.3** `transactions` değişmez | yeni sütunlar yalnız INSERT; `client` ve sonraki kanıt sütunları tarihsel satırda NULL; `Down` dolu satırda reddeder (M15 deseni) | mevcut R3 |
| **4.4** atomik `ctr` | değişmez — SUN sunucuda `sun.Verify` ile doğrulanır; nonce tüketimi de tek ifadeli | mevcut R4 + nonce için eşzamanlılık testi |
| **4.5** tenant izolasyonu | `app_devices`, nonce ve ekran tabloları §6 beşlisiyle (tenant_id, ENABLE + FORCE RLS, politika, indeks, GRANT) | mevcut R5 |
| **4.6** kayıt kaybolmaz | uygulama yolu ile tarayıcı yolu aynı `Record`'a iner; `qr_live` ve `client` kayıt yazan her okuyucuya (rapor, akış, liste) girer | kanal envanteri (M16-19) |
| **4.7** AES anahtarı repoda yok | uygulama hiçbir plaket anahtarı taşımaz, SUN'u doğrulamaz; canlı QR imza anahtarı yalnız sunucu sırrıdır; mağaza imza anahtarları repoda yok | R7 / R7d `mobile/`'ı da tarar |
| **§7 log** | uygulama ve sunucu log'u (hata mesajı ve `audit_log` `detail`'i dahil) cihaz imzası, istek nonce'u, canlı QR token'ı, ekran eşleşme kodu, ekran oturumu çerezi, canlı QR imza anahtarı, müdür onayı eşleşme kodu, tam koordinat, davet kodu, e-posta yazmaz — CLAUDE.md §7 listesi M16-01'de genişler | R7 kalıpları Swift/Kotlin log çağrılarına genişler |

## 4. Bedeller

1. **İki yeni kod tabanı ve iki mağaza süreci, tek kişilik ekip** (roadmap risk tablosu). Her işletim sistemi
   sürümü ve mağaza politikası değişikliği bakım ister.
2. **Mağaza bağımlılığı.** Apple ve Google hesabı, inceleme, gizlilik etiketleri; inceleme bir sürümü reddedebilir ya
   da geciktirebilir. İnceleme bir demo hesabı ister (aktive bir çalışan + plaketsiz çalışabilen bir yol).
3. **Buddy punching çözülmez** (ADR 0005 risk 1). Cihaz anahtarı kopyalamayı durdurur, devretmeyi değil.
4. **Sahte konum bildirimi yalnız dürüst istemciyi yakalar.** Değiştirilmiş bir APK ya da konum sahteleyen bir
   araç bildirmeyebilir; attestation (Faz E) bunu yalnız **raporlar**.
5. **Canlı QR aktarılabilir.** ~20 sn içinde bir fotoğraf ya da görüntülü arama ekranı uzaktaki birine taşır.
   Varsayılan politika IP **veya** GPS ister; uzaktaki kişi mekânın IP'sini taşımaz ama GPS sahteleyebilir (A3) →
   `ok` olabilir. Kabul edilen bir risk olarak ADR 0005'e eklenir (append kuralı; `TestADR0005_TheRiskCountMatchesTheTable`
   sayıyı tabloyla eşler).
6. **Kanal sözlüğünü genişletmek geniş bir yüzeydir — ölçüldü (okundu, `70e8a016`).** `nfc|qr|manual` kapalı
   sözlüğü 10 migration'da (`00005`, `00043`, `00044`, `00046`, `00048`, `00049`, `00050`, `00067`, `00068`, `00095`),
   2 sorgu dosyasında (`transactions.sql`, `daycutoff.sql`) ve ~12 Go dosyasında (`checkin.go`, `guardrails.go`,
   `baseline.go`, `tap.go`, `sandbox/simulate.go` …) yazılı. İkisi sessiz kayıp üretir: akış tetikleyicisi
   `NEW.channel NOT IN ('nfc', 'qr')` satırını atlar (`00067:193`) ve Go aynası `feedEligible` aynı şeyi söyler
   (`internal/domain/checkin/checkin.go:1687-1689`); `/v1` şeması kanalı `enum: [nfc, qr]` diye kapatır
   (`internal/handler/api/openapi.yaml:1105`, `:1170`). Ayrıca `sys:tap-freshness` ve `sys:sun-invalid` yalnız NFC'de
   koşar (`internal/policy/guardrails.go:396`, `:479`). Debounce'un sunucu saati bacağı `SecondsSinceLastRecordedTap`
   de `channel IN ('nfc', 'qr')` ile süzer (`db/queries/transactions.sql:317`): `qr_live` girmezse geriye tarihlenmiş
   bir canlı QR dokunuşu debounce'u **sessizce** atlar. Motor da her tap'te bir plaket varsayar
   (`internal/domain/tap/types.go:471-473`). Faz D bu envanteri tek kartta kapatır (M16-19, M16-21).
7. **Müdür onaylı aktivasyon fiziksel dokunuş kanıtı taşımaz.** Sosyal mühendisliğe açıktır (davet linki başkasının
   elinde); eşleşme kodu ve `audit_log` bunu daraltır, kapatmaz. Kimliği müdür doğrular.
8. **Kurulu uygulama plaket URL'sini yakalar.** Uygulama bozuk ya da eskiyse çalışanın tarayıcıya düşmesi bir
   işletim sistemi hareketi ister (iOS'ta uzun basma). Uygulama bir *"tarayıcıda aç"* çıkışı sunmalıdır — bu ekran
   bir §9 sorusudur.
9. **"No app" cümleleri.** Landing sloganı (`web/templates/pages/landing.templ:136`) ve *"No app to install"*
   iddiaları (`productview.go:291`, `forproductionview.go:330`, davet e-postası `email/letter.go:68`) uygulama isteğe
   bağlı olduğu için **doğru kalır**, ama konumlanma sorusu kullanıcınındır (Q42).
10. **Yeni baseline belgesi var olan tenant'lara onaysız girer.** Okundu: tap yolu eksik baseline belgelerini ilk
    dokunuşta yazar (`internal/domain/checkin/policyset.go:422` `assembleBaseline` → `:529` `materialise`); M7-03'ün
    *"duyur → kabul et"* akışı yoktur (`internal/policy/baseline.go:50-111`). `base:mock-location-review` yalnız
    uygulamanın gönderdiği bir anahtarda eşleştiği için uygulamasız tenant'ta hiçbir sonucu değiştirmez; yine de
    `BaselineVersion` artar ve `BaselineChanges` bir duyuru taşır (`TestBaseline_ContentIsPinnedToItsStamp` bunu
    zorlar).
11. **Cihaz anahtarı kaybolur.** Telefon sıfırlama, yeni telefon ya da yedekten geri yükleme anahtarı taşımaz
    (bilinçli: *ThisDeviceOnly*). Çalışan yeniden aktive olur (yeni davet ya da müdür onayı).
12. **Attestation dışa bağımlıdır.** App Attest Apple'a, Play Integrity Google'a bağlıdır; Play Integrity için
    sunucuda Google'a çağrı yerine yerel doğrulama tercih edilir (Q37). Kanıttan öte değeri yoktur (§2.7).
13. **İki Android dizini.** `android/` (röle) ve `mobile/android/` (personel uygulaması) ayrı Gradle build'leridir;
    karışıklık bedeli dizin haritası ve README ile ödenir.

## 5. Okunanlar ve doğrulanacaklar

**Okundu (`70e8a016`):** yukarıdaki her `file:line` · `transactions` INSERT'i sütun listesidir (ADR 0038) → yeni
sütun yeni bir sütun GRANT'ı ister · `redline-check.sh` `SRC` listesi `mobile/`'ı içermez (`scripts/redline-check.sh:33`),
B1 marka kuralı `android/app/src/main`'i tarar (`:2530`) · CI hiçbir mobil derleme koşmaz (`.github/workflows/*.yml`'de
`gradle`/`xcode` yok) · son migration `00099`, son ADR `0043` (`0040` numarası hiç kullanılmamış).

**Doğrula (ilgili kart ölçer):** iPhone XS+ arka plan okumasının Universal Link ile kurulu uygulamayı açması (M16-17) ·
Android NFC dağıtımının `NDEF_DISCOVERED` filtresiyle uygulamayı tarayıcıdan önce seçmesi (M16-13) · iOS sahte konum
bayrağının gerçek davranışı (M16-17) · iPhone 7'nin desteklediği son iOS sürümü → dağıtım hedefi (M16-15) · bu makinede
Xcode sürümü ve iOS derlemesinin mümkünlüğü (M16-15) · Play Integrity yanıtının yerel doğrulanabilirliği (M16-25).

## 6. Kabul

M16-01'de: kullanıcı onayı, Q35–Q42'nin cevapları (ya da hangi kartı blokladıklarının yazılması), durum satırı
*Kabul edildi (TARİH)*.
