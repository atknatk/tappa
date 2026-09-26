# M10 — Platform: acil güvenlik, operatör kimliği, e-posta (SES), white-label

> **Öncelik:** kullanıcı kararı (2026-09-24) — bu dosyadaki işler M9'un geri kalanından ve yeni
> özelliklerden ÖNCE çözülür (*"bu tarz problemler önce çözülmeli"*).
> **Kaynak:** üç bağımsız mimar ajanı (salt-okuma, HEAD `a57d6e7`) + orkestratör sentezi. Satır
> numaraları o commit'e aittir; uygulama anında yeniden doğrulanır.
> **Durum işareti bu dosyada tutulmaz** — canlı durum `state.md`. Kararlar §6'da: ⏳ kullanıcı
> onayı bekliyor, ✅ önerisiyle uygulanır (otonomi kuralı; itiraz edilirse değişir).

## 0. Faz 0 — Acil güvenlik (tasarımdan bağımsız, hemen)

### Olay A-1 (2026-09-24, canlı pilot)
Yakılmış çip `0492A2BA902390` (23. oturumda encode edilen, anahtarları silinmiş eski tenant'a ait) bugün tekrar encode denendi → `writedata` **91AE** ile düştü (yeni yapılandırılmış log sayesinde görüldü), step 3'te yazılan satır `encoded_at` boş kaldı — ve panel buna **mount'a izin verdi**: **Rusty Bar**'da duvarda, **12 tap / 0 geçerli** (DB'deki yeni anahtar çipe hiç yazılmadı → her SUN reddediliyor). Aynı gün iki yeni çip (KF St Julians, KF Paceville) tam encode + **anahtar-0 döndürülmüş** → md.5 gerçek silikonda da KAPANDI. **Kullanıcıya:** Rusty Bar plaketini yeni boş çiple değiştir (panel → plaket → replace). Kod tarafı: F0-6 (mount kapısı) + F0-7 (91AE imzası).

### Olay A-0 (2026-09-24'te tespit)
Operatör hesabının (allow-list'teki tek id, Kebab Factory Ltd. owner'ı) canlı panel parolası,
bir "rotate edilmeli" borç notunun İÇİNDE `state.md`'ye yazılmış (4 satır, 3 commit,
2026-09-19'dan beri pushlanmış). **Depo `atknatk/tappa` PUBLIC.** Hesap `/admin/legal` üzerinden
Taptime'ın herkese açık gizlilik/künye metinlerini değiştirebilir ve tenant verisini okur.
- **Triage (2026-09-24, salt-okuma):** hesabın 9 oturumunun ve 9 `admin.login.succeeded`
  satırının hepsi bilinen etkinliğe (19 Eylül kurulum otomasyonu + kullanıcının kendi cihazları)
  karşılık geliyor · 20 Eylül 08:44'teki tek istek MEVCUT bir oturum çereziyle `GET /admin`
  (parolayla yeni oturum değil) · başarısız giriş 0 (pod logu 2026-09-20 08:25'ten beri) ·
  19 Eylül'den beri tek yasal yayın bizim künye · audit'teki her eylem kullanıcının kurulumu.
  **Yetkisiz kullanım kanıtı yok.**
- **Yapılan:** değer HEAD'den kaldırıldı (`920baaa`). DB parolaları ve eski panel parolası
  repoda/geçmişte YOK (ölçüldü) — ama sohbette açığa çıktılar, rotate borcu sürüyor.
- **Kök neden:** sırrın DEĞERİ bir kayıt dosyasına yazıldı. Kural: sırra yalnız adıyla atıf.

| ID | Görev | Kim | Kabul |
|---|---|---|---|
| F0-1 | Operatör panel parolasını değiştir (Account → Change password, T73). Yeni parola sohbete/repoya yazılmaz | **Kullanıcı** | eski parolayla giriş 401; diğer oturumlar iptal (K3) |
| F0-2 | Scrub commit'ini push et | orkestratör (kullanıcı onayıyla) | `origin/main`'de değer 0 |
| F0-3 | Depo görünürlüğü — ✅ **karar: PUBLIC kalır** (D-A). Geçmişteki değer yalnız F0-1 ile ölür | **Kullanıcı** | — (karar verildi) |
| F0-4 | `tappa_app` + `tappa_owner` DB parolalarını rotate et (tappa-secrets + `ALTER ROLE`) | **Kullanıcı** (§4.7: ajan tappa-secrets'a dokunmaz) | `/readyz` 200; eski parolayla bağlantı reddi |
| F0-5 | Sır sızıntısı kapısı: `redline-check.sh`'e kural — commit'lenen her dosyada kimlik bilgisi biçimleri (`postgres://…:…@`, `AKIA…`, `$2a$..$` digest, uzun hex parola biçimleri, `password=`) → FAIL; + agent-brief sabit kurallarına "sır DEĞERİ hiçbir dosyaya yazılmaz" | builder + güvenlik | mutasyon: state.md'ye sahte `postgres://u:p@h/db` eklemek redline'ı kırmızıya çevirir |
| F0-6 | **Mount kapısı:** `encoded_at IS NULL` olan plaketin mount/replace'i REDDEDİLİR (ADR 0017 §5.1 "anahtar 0 fabrikadayken duvara çıkamaz" güvenlik çizgisini KODDA uygular). Olay: 2026-09-24 yakılmış çip (`writedata` 91AE ile yarım kalmış satır) Rusty Bar'a mount edildi → 12 tap, 0 geçerli | builder + güvenlik | encode edilmemiş plaketin mount/replace POST'u 303 + ret cümlesi + audit, 0 UPDATE; encode edilmiş plaket etkilenmez; plaket kartı "encode tamamlanmadı — duvara takılamaz" der |
| F0-7 | `isReEncodeRejection`'a gerçek silikon imzasını ekle: **`writedata` + `91AE`** (AUTHENTICATION_ERROR — ilk encode'un step 7'si NDEF yazma yetkisini kilitler; 2026-09-24 ölçüldü). Bugün jenerik `refused` gösteriyor | builder | handler tablo testi: writedata+91AE → `already-encoded`; writedata+917E → `refused` kalır |
| OP-2 | Allow-list parser birim testleri (A-5) | builder | boş/boşluk→nil; `a,,b`→hata; e-posta→hata; nil uuid→hata; tekrar→tek; nil-reddi mutasyonu kırmızı |
| OP-3 | `tenants` UPDATE yetkisini daralt (A-4): `REVOKE UPDATE` + `GRANT UPDATE (name, business_type, timezone)` | tappa-db-migrator + güvenlik | `has_column_privilege(tappa_app,'tenants','vat_number'\|'structure','UPDATE')=false`; kapalı-liste testi genişletildi; signup E2E yeşil; Down temiz |

> **Kart düzeltmesi (2026-09-26, OP-3 uygulaması sırasında).** Migration **00024**. (1) UPDATE
> 00016'dan beri zaten sütun düzeyindeydi (ölçüldü, dev = üretim: `vat_number`/`structure` = `aw`),
> yani pratik etki bu iki sütundan UPDATE'in düşmesi; tek yazan ifade (`UpdateTenantAccount`)
> yalnız `name`/`business_type`/`timezone` yazıyor. (2) **Kapsam genişletildi (orkestratör
> kararı, en-az-yetki):** tablo düzeyi `DELETE` de REVOKE edildi — `DELETE FROM tenants` hiçbir
> yerde yok (grep 0), ama çocuksuz yeni bir tenant satırını `tappa_app` silebiliyordu (ölçüldü:
> `DELETE 1`; sonra 42501). (3) Sıra yük taşır: tablo düzeyi `REVOKE UPDATE` sütun grant'larını
> da siler (ölçüldü) — REVOKE'tan sonra GRANT (kapalı liste) gelmezse hesap ekranı kırılır.
> INSERT listesine dokunulmadı. Down, 00016+00017 ACL'ini bayt-aynı geri kuruyor.
>
> **F0-6b (aynı gün, migration 00025).** F0-6'nın uygulama kapısının altına şema kemeri:
> `tags_active_requires_recorded_encode` — damgası ifadeden ÖNCE kayıtlı olmayan bir plaketin
> `active`'e **geçişi** her rolde (tappa_owner dahil) 23001 ile reddedilir. `active` kalan damgasız
> satırın (Rusty Bar) `last_ctr` artışı, retire ve unmount'u geçer (§4.6). **INSERT yarısı
> BİLEREK yok:** `tappa_app` tablo düzeyi INSERT tutuyor ve `status` DEFAULT'u `'active'` —
> damgasız `active` satır INSERT ile üretilebiliyor (ölçüldü); ama 00022 her satırı damgasız
> doğurduğu için bir INSERT trigger'ı her `active` INSERT'i reddeder: seed.sql'in plaket INSERT'i
> yeniden koşumda bile kırılır (ölçüldü) ve `internal/store` dışında 17 Go dosyasında 51
> `INSERT INTO tags` satırı (çoğu `active` yükleyen fixture) yeniden şekillenmeli. Açık kalan, T16
> ile birlikte ayrı bir iş.

> **Kart düzeltmesi (2026-09-25, F0-5 uygulaması sırasında).** Kural kodu **R7d**, R8 değil:
> redline R1–R7 `tappa-security-auditor`'ın R1–R7 başlıklarıyla birebir eşleşiyor ve o ajanın
> R8'i "Karar sırası uyumu" (plan belgelerinde 9 kez "auditor R6/R8" diye geçiyor). Desenler,
> tetikleyiciler ve muafiyet tablosu tek yerde, `scripts/secretscan.sh`; iki tüketicisi var:
> `redline-check.sh` R7d (commit'lenebilir **her** dosya — `docs/` dahil, `.env` hariç) ve
> `scripts/git-hooks/pre-push` (push edilecek aralığın **eklenen** satırları + commit mesajları;
> kurulum `make hooks`). Kartın "uzun hex parola biçimleri" maddesi a0-token'ın ≥32 haneli hex
> alt-şekli olarak var. Bulgu metni hiçbir çıktıya basılmaz (`yol:satır: [sınıf]`). Regresyon
> ağı `cmd/tappa/secretscan_test.go`. agent-brief maddesi `573d4e1`'de zaten yazılmıştı.
> 2. tur (üçüncü göz RED): muafiyet belirteçleri artık sınırlı eşleşiyor, commit-mesajı
> muafiyeti tek commit'e bağlı, kanca boru hattının her aşamasını
> ayrı okuyor, kayıt ayıracı `:` değil 0x1F, kanca `--text` + annotated tag mesajı okuyor,
> sağlayıcı önekleri / yeni atama anahtarları / `IDENTIFIED BY` / `curl -u` eklendi. Yan
> bulgu: GNU `mktemp -t ad` X'siz şablonu reddeder — `redline-check.sh`'in `SCAN_ERR`
> işaretçisi Ubuntu CI'da yazıldığından beri ölüydü; şablonlu yolla düzeltildi.
> 3. tur (üçüncü göz RED, B1 kalıntısı): 2. turun "sınırlı eşleşme"si yalnız ardındaki bir
> *değer karakterine* bakıyordu ve YAML'da boşluk/sekme/`,`/`;`/tırnak/`(`/`\`, kabukta
> tırnak/`\`, SQL'de `''` ile uzatılmış dev değerini hâlâ affediyordu (denetçi sekiz ayraçla
> ölçtü). Muafiyet belirteçleri TÜRLÜ yapıldı (`@yaml` / `@sh` / `@sql`) — 4. tur bunu da kırdı
> (aşağıda); o makine 4. turda silindi. Kanca: ikili kararı blob'dan (0x01 işaretçisi değil), `--root`, 8
> kat tag sınırı testle pinli; CI'da `make audit` önceki adımlar kırmızı olsa da koşar.
> Push notu: kanca aralığı uzağın ADIYLA hesaplar — `git push -u origin m10-faz0` taklit bir
> origin'e karşı rc=0 (4 yeni commit); URL ile push tüm geçmişi tarar ve A-0 satırları
> yüzünden exit 1 verir (pre-push başlığı ve Makefile `hooks` notu).
> 4. tur (üçüncü göz RED): `@yaml`/`@sql` muafiyeti, satır sonunda BİTMEYEN değeri (YAML çok
> satırlı düz skaler, SQL bitişik dizge; PyYAML/Ruby/yq ve Postgres 17 ile ölçüldü) hâlâ
> affediyordu — üç turdur aynı sınıf. Kök neden `tappa` gibi zayıf bir dev değerini önce
> yakalayıp sonra affetmekti. Karar ve uygulama: `pw-assign`'ın bütün biçimleri değerin gücüne
> bakıyor (≥6 karakter, ≥2 sınıf, yer tutucu/başvuru değil); değer dilin kuralıyla okunuyor
> (kabuk kelimesi, YAML satır sonu, SQL `''`). Dev değeri muafiyetleri ve tür makinesi
> silindi; ağaçta tablo boşken bile 0 `pw-assign` isabeti. Zayıf değer ve satırlar arası
> devam, `scripts/secretscan.sh`'te sayılı sınır. Varsayılan sınır da sıkılaştı: kapanan
> tırnaktan sonra yalnız satır sonu ya da `,;)]}` (Go dizge birleştirmesi `" + "` artık
> affedilmiyor); backtick yalnız .md'de ve yorum satırında sınır.
> 5. tur (üçüncü göz RED): tablodaki tek `@prefix=` satırı (rotatekek testi) açılış
> tırnağını da çıkarıyordu; satırın tırnak paritesi kayıyor ve aynı satıra eklenen ikinci
> tırnaklı değer üç biçimde sessiz kalıyordu. 4. turun ilkesiyle muafiyet sıkılaştırılmadı,
> KALDIRILDI: test değeri çalışma anında 8 karakterden kısa parçalardan kuruluyor (bayt
> bayt aynı), `@prefix=` türü silindi, tırnak taşıyan belirteç tabloyu exit 2 ile reddediyor.
> "Aynı satırda ikinci değer FAIL verir" bir teste bağlandı (6. turda düzeltildi: dört
> biçimden üçünde belirteç sınırlı değildi; 7. turda belirteç mekanizmasıyla birlikte
> silindi, aşağıda). İstisna, sayılı: `@class` (htmx) o sınıfın ikinci değerini de affediyordu
> (7. turda silindi). Değer okuyucuları: YAML tırnaklı skaler kaçışları, düğüm
> özellikleri (`&çapa`, `!etiket`), tırnaklı değerde başvuru süzgeci yok, `$`/`%` + güçlü
> gövde değer (6. turda daraltıldı: bu kural yalnız `$`'ı AÇAN bağlamda; kısa kuyruk orada
> sayılı sınır); kanca `GIT_NO_REPLACE_OBJECTS=1`. Tam geçmiş taramasında A-0'ın 9 satırına
> rotatekek testinin `a32d0ca`'daki tek satırı eklendi (artık muaf değil; yalnız tüm geçmişi
> tarayan URL/yeni-uzak push'unda görünür, o da A-0 yüzünden zaten exit 1).
> 6. tur (üçüncü göz RED): muafiyet belirteci silinince içindeki sır kelimesi de
> gidiyordu — ADR 0019'un örnek parolası ve CI-only sahte KEK "password"/"secret"
> kelimesini kendisi taşıyor; satıra eklenen ikinci güçlü değer, kelime yalnız belirteçte
> olduğunda sessizdi. Mekanizmada çözüldü: silme satır uzunluğunu koruyor, bağlam (sır
> kelimesi, atama anahtarı, `curl`) orijinal satırdan, değer adayları silinmiş satırdan
> okunuyordu; ikinci-değer testi eşli kontrollerle yeniden yazılmıştı (ikisi de 7. turda
> belirteç mekanizmasıyla birlikte silindi).
> `$`/`%`: düz bağlamda (.md, commit/tag mesajı, kaynak kod, URL parolası) `$` hiçbir şey
> açmaz — `$AD`, `${...}`, printf biçimi dışındaki her şey `$` dahil tam değer olarak
> ölçülüyor; açan bağlamda kısa kuyruk sayılı sınır (#23). Grafts ve değersiz YAML çapası
> sayıldı (#10, #18).
> 7. tur (üçüncü göz RED; orkestratör kararı: yeniden tasarım): 6. turun boşluğa çevirme
> mekanizması SOL uzatmayı açmıştı (22 belirteç × 5 ayraç × 3 tırnak = 330 satırın 330'u
> adlı yolda sessiz; signup.go'ya eklenen bir satır push'ta rc=0). Altı turun bloklayanının
> hepsi aynı sınıftı: satırın İÇİNDEKİ bir belirteci affedip geri kalanını sınıflamak. Muafiyet
> artık SATIRIN TAMAMINA bağlı (redline R1'in "cümleye bağlı" ilkesi): tablo girdisi
> `yol ERE;sha256(satırın tam baytları, \n hariç);açıklama` — değer içermez; hash tutan satır
> hiç sınıflanmaz, bir bayt değişirse adsız bir yoldaki gibi sınıflanır. Belirteç silme,
> sınır kuralları, bağlam aktarımı, `@prefix`/`@class`, tablonun kendi satırı istisnası ve
> bunların testleri silindi. Tablo, 6. turun muaf ettiği 37 ağaç satırından üretildi: 32
> girdi; yeni muaf listesi eskisiyle birebir aynı. htmx girdisinin hash'i README'deki
> sha256'nın kendisi (tek satırlık dosya). Özellik testi: muaf her satırın her
> tırnak/ayraç komşuluğuna, başına, sonuna ve 10 rastgele konuma güçlü bir değer eklenir —
> 1178 satırın 1178'i adsız yoldakiyle birebir aynı sınıflanıyor (aynı küme 6. turun
> mekanizmasında 792 satırda sessizdi); tek bayt değişikliği (sondaki boşluk, `\r`, …) muafiyeti
> düşürüyor. Muafiyet eklemek: `bash scripts/secretscan.sh --hash <dosya>:<satır>` (satır
> metnini basmaz). Ayrıca kabuk kelimesi kapanan `"`'ın ardından kelime/tırnak gelirse
> sürüyor; deploy/k8s YAML'ında yalnız `$(AD)` açılıyor. Tamlık iddiası yok: sayılı sınırlar
> `scripts/secretscan.sh` başlığında.
> 8. tur (üçüncü göz ONAY; bloklamayan bulgular kapatıldı): kanca ÖLÇÜLEN git
> ayarlarından bağımsız okuyor (`--src-prefix=a/ --dst-prefix=b/`, `--encoding=UTF-8` ve
> diğer bayraklar; `diff.dstPrefix` ve UTF-16 log kodlaması altında rc=0 ölçülmüştü;
> tamlık iddiası yok — 9. turda iki ayar daha bulundu, aşağıda).
> Tablo açıklaması değer taşıyamıyor, yol tek bir dosyayı adlandırmak zorunda, `--hash`
> yardımcısının `\r`/sondaki boşluk sadakati testle pinli, redline R7d FAIL mesajı
> yardımcıyı gösteriyor, boşluksuz printf biçimi (`KEY=%-20s`) kod şekli. Sayıldı:
> boşluklu a0 adayı (#24), yalan söyleyen araçlar (#25).
> 9. tur (üçüncü göz RED): 8. turun printf kuralı (`ANAHTAR=` + harf/rakam + tek bir
> `%<harf>` → kod) güçlü değerleri susturuyordu (7. turda kırmızı 13 değer sessizdi).
> Kural artık yalnız printf fiilleri + alfanümerik olmayan ayraçlardan oluşan değeri kod
> sayıyor; sabit tohumlu 2 × 300 rastgele değerde kural açıkken ve kapalıyken aynı satırlar
> raporlanıyor. Ayrıca: tablo açıklaması boşluk kelimeleriyle de sınanıyor; tablo hatası
> satır metnini basmıyor; kanca `diff.interHunkContext`/`GIT_DIFF_OPTS` bağlam satırlarında
> doğru satır numarası veriyor ve yanlış `encoding` başlıklı commit mesajını ham nesneden
> okuyor. Muafiyet yolu kısıtı belgelendi (#26: böyle bir dosya gerekirse yeniden adlandırılır).
> 10. tur (üçüncü göz ONAY; yalnız belge ve test): printf kuralının susturduğu iki biçim
> (isimli fiildeki tanımlayıcı, yalnız fiillerden oluşan değer) sınır #27; kuralın testi
> yalnız kendi tohumlu örneklemini iddia ediyor (`TestSecretScan_ThePrintfRuleSilencesNoRealisticValue`).
> 128 karakterden uzun a0 adayı sınır #28. İmzalı bir tag'in `git merge` ile birleştirilmesi:
> tag mesajı merge commit'in `mergetag` başlığına gömülüyor ve yayınlanıyordu; kanca artık o
> başlığın devam satırlarını okuyor (ssh imzalı tag + özel merge mesajında rc=0 → 1, ölçüldü).
> Ham mesaj geçişinin iki koruması (kesilmiş `--batch` akışı, gerçek UTF-16LE mesaj baytları)
> testle pinlendi. Bilinen gürültüye Go'nun `%[1]s` ve `100%%s` biçimleri eklendi.
> 11. tur (tappa-security-auditor ONAY; ORTA §4.7 kapatıldı): KEK ve NTAG AES anahtar
> değerleri A-0 biçiminde yazılınca sessizdi ("canlı KEK", "plaket anahtarı (key 1)",
> `TAPPA_TAG_KEK=`, `*_HMAC_KEY=`; altı biçimin altısı). Anahtar bağlamı kelimeleri (kek,
> hmac, aes, key, anahtar) artık YALNIZ anahtar biçimli tırnaklı bir değerle birlikte
> tetik; `kek` ve `hmac_key` atama anahtarı — örnek Secret'ın yedi adının yedisi bir
> kurala giriyor. Tablo boşken ağaçta 23 yeni satır çıktı; hepsi sınıflandırıldı (CI'nın
> belgelenmiş sahte KEK'i, AN12196 bilinen-cevap vektörleri, `_label: FAKE` fixture),
> gerçek sızıntı yok; satıra bağlı 18 girdiyle muaf. Kesilen bir taramanın geçici
> dizini (satır metni taşır) artık siliniyor. Sayıldı: commit başlık alanları (#29),
> anahtar bağlamının sınırı (#30). `make audit` etiketi "SKIPPED(scan could not run)".
> 12. tur (son sertleştirme): `kek`/`hmac_key` adın ortasında bir rotasyon sonekiyle de
> atama anahtarı (`TAPPA_TAG_KEK_PREVIOUS=`); camelCase adlardaki anahtar kelimesi
> (`tagKey`, `prodKEK`) bağlam. Tablo boşken ağaçta 14 yeni satır — hepsi test değeri
> (AN12196, belge dışı bir bayt rampası, fake/test/wrong adlı değerler, sun_vectors.json
> sahtelerinin kopyaları),
> gerçek sızıntı yok; satıra bağlı 14 girdiyle muaf (tablo 64 girdi). Her bağlam kelimesi
> kendi vakasıyla pinli. Kancanın üst düzey HUP tuzağı ölçüldü: Ubuntu bash 5.2'de tuzaksız
> 12/20 koşuda kalıntı, tuzakla 0/20 — tutuldu, test onu yalnız olasılıkla pinliyor (#31).
> Sayıldı: #31, #32 ve #30'a eklenen anahtar biçimleri; bilinen gürültüye beş biçim.
> Kapanış (yalnız metin): ayraçsız ve camel sınırsız adlar (`TAGKEY`, `tagkey`) #33.
>
> **Kart düzeltmesi (2026-09-25, OP-2 uygulaması sırasında).** `a,,b` hata verir ama boş-eleman
> reddi yüzünden DEĞİL: `a` bir uuid olmadığı için ilk elemanda düşer. Boş-eleman reddini ölçen
> vaka `<uuid>,,<uuid>` (ve sondaki virgül) — hata mesajı `empty entry` ile doğrulanır; yalnız
> "hata var" demek, ret silindiğinde de `uuid.Parse("")` yüzünden yeşil kalırdı.

## 1. Kullanıcının endişesinin denetimi — hüküm
*"Her restoran admini sistem verisini güncelleyebiliyor mu?"* → **Kodda yetki açığı YOK.** 24 panel
POST rotasının tamamı tek tek denetlendi:
- Yasal metinler (Taptime künyesi dahil — kullanıcının gördüğü *"company details"* bu sekmenin
  açıklaması, `adminview.go:354-365`) yalnız allow-list'teki tek hesaba açık; diğer herkes GET ve
  POST 403 (`TestOperatorGate_TheServerRefusesTheRouteItself`, `…EmptyAllowListAdmitsNobody`,
  `…IsNotJoinableByRegisteringABusiness`).
- `sys:*` guardrail'ler kodda, DB'de satırı yok (`TestPolicies_LayerCheckRejectsGuardrail`); plan/
  fiyat/VAT doğrulama sütunları `tappa_app`'e kapalı; başka tenant FORCE RLS + açık filtre ile
  görünmez (`TestRLS_ReadIsolation_AllTables`, `TestRLS_AppRoleHasNoBypass`).
- Account'taki ad/tür/saat dilimi tenant'ın KENDİ satırı — meşru.

**Asıl sorun yapısal:** platform gücü bir restoran hesabına bağlı (allow-list kimliği = KF owner'ı)
→ o hesabın parolası sızınca (A-0) platform açığı oluyor; MFA yok, sunucu tarafı oturum süresi yok
(`adminauth/cookie.go:44-73`), operatör audit'i restoranın tenant'ına yazılıyor. Akış A çözer.
Küçük bulgular: A-2 encode global uid işgali (ADR 0017 md.12, bilinen) → OP-18 · A-3 signup'ta VAT
işgali → OP-11/15/16 · A-4 → OP-3 · A-5 → OP-2 · A-7 encode step handle admin'e bağlı değil → OP-18.

## 2. Sıralama — ✅ D-D (2026-09-24)
```
Faz 0 (bugün) ─► A1 Operatör kimliği + legal taşıma (OP-4..OP-10)
                 ║ paralel: kullanıcının SES dış adımları (AWS / DNS / sandbox çıkışı — günler sürer)
              ─► B  E-posta: taşıyıcı → reset → davet (EM-1..EM-8)
              ─► C  White-label (WL-0..WL-12)
              ─► A2 Tenant-ötesi okuma/yazma: liste, fatura, askı, VAT, plaket (OP-11..OP-18)
```
Gerekçe: (1) A-0 tam olarak "platform gücü restoran hesabında" sınıfının sonucu — önce o kapanır;
(2) SES'in dış adımları gün alır, erken başlamalı; reset e-postası owner'ın hesap kurtarması (bugün
DB'siz yol yok); (3) white-label ürün cilası ve §9 kararına bağlı; (4) A2 çoklu-müşteri operasyon
araçları — pilot büyüdükçe. Akışlar paralel ÇALIŞTIRILMAZ (paylaşılan Postgres, sıralı denetçi) —
kullanıcının dış adımları hariç.

**Numaralandırma:** her ADR/migration yazıldığı anda sıradaki boş numarayı alır (bugün ADR 0020,
migration 00024). Önerilen ADR sırası: 0020 operatör kimliği · 0021 `op_*` arayüzü · 0022 e-posta
taşıyıcısı · 0023 tenant markası/§9 · 0024 kullanıcı yüklediği görsel · 0025 tenant askıya alma.

**Akışlar arası çakışma çözümleri:**
- **E-posta gönderen adı:** faz 1'de SABİT `Taptime <no-reply@taptime.mt>` (EM-K5). WL planının
  "X via Taptime" önerisi ertelendi — tenant adı herkese açık signup'la saldırgan kontrolünde →
  oltalama kaldıracı; ancak VIES-doğrulanmış tenant koşuluyla, WL-11 kapsamında.
- **Panel kabuğu:** OP-10 (TabLegal/Operator kaldırma) WL-8'den (logo/şerit) ÖNCE.
- **Askı (OP-15):** `ProtectWriting` içindeki `suspensionGate` WL marka rotalarını ve EM davet
  rotasını otomatik kapsar; OP-15 kabulü router'dan türetilen rota listesiyle olduğu için yeni
  rotalar kendiliğinden teste girer.

## 3. Akış A — Platform operatörü (süper admin)

### Karar: (c) hibrit — ✅ D-B (2026-09-24)
Ayrı `platform_admins` kimliği (ayrı tablo, çerez, oturum, zorunlu TOTP, sunucu tarafı oturum
süresi), `/operator` ağacı tercihen `ops.taptime.mt`; tenant-ötesi erişim YALNIZ ayrı bir LOGIN
rolünün (`tappa_operator`, NOBYPASSRLS) çağırabildiği, adı konmuş `SECURITY DEFINER op_*`
fonksiyonlarıyla; tek binary, tek deployment. **Önceki "B — allow-list'i genişlet" kararının yerine
geçer.** Gerekçe: M9-08'in 4. kabul kriteri (*"müşteri oturumu /operator'a hiçbir koşulda
giremez"*) (a) ile yapısal olarak karşılanamaz (operatör oturumu = müşteri oturumu) · ADR 0016 §2
tam olarak (a)'nın gerektirdiği deseni (*"müşteri kimliğiyle ulaşılan handler'da
`WithTenant(başkası)`"*) yasaklıyor · A-0 · ADR 0016 §5'in "yeni giriş gereksiz" gerekçesi dört
yasal belge içindi, M9-08'in kapsamı §4.5'i aşan beş işlev.

| Kriter | (a) allow-list | (b) ayrı süreç/deploy | **(c) hibrit** |
|---|---|---|---|
| Müşteri oturumu → operatör yüzeyi | aynı çerez, handler başına kapı | yapısal olarak imkânsız | yapısal olarak imkânsız |
| MFA / oturum süresi | yok | var | var |
| Tenant-ötesi DB | `WithTenant(başkası)` ya da bypass | rol + definer | rol + definer |
| Signup'tan sömürü | her tenant sahibi potansiyel saldırgan | yok | yok |
| Aynı süreçte RCE | her şey | operatör DSN'ine ulaşılamaz | ulaşılır (K9 ile kapanır) |
| Efor | düşük başlar, her ekranla artan risk | en yüksek | orta–yüksek |

### Tasarım özü (ADR 0020/0021'de normatif olacak)
- **Kimlik:** `platform_admins` — email citext GLOBAL UNIQUE, bcrypt cost 12, parola ≥14 rune,
  `totp_secret_sealed` (AES-256-GCM, ayrı KEK `TAPPA_OPERATOR_TOTP_KEK`), `totp_last_step` (tekrar
  koruması §4.4'ün aynası: `UPDATE … WHERE totp_last_step < $2`, 0 satır → ret), status
  pending/active/disabled, CHECK active⇒parola+TOTP. `platform_sessions` — mutlak 8 saat, boşta
  30 dk, `mfa_verified_at`, `revoked_at`; touch tek ifade, fail-closed. Çerez `__Host-taptime_op`
  (Secure, HttpOnly, SameSite=Strict, Domain yok), ayrı HMAC etiketi, farklı redaction placeholder.
- **Giriş:** e-posta+parola (bilinmeyen e-postada sabit bcrypt) → kısa ömürlü imzalı ara çerez →
  TOTP → oturum. TOTP stdlib (RFC 6238, HMAC-SHA1, 6 hane, 30 sn, ±1 adım); QR kütüphanesi yok —
  base32 + `otpauth://` elle. flood/attempt/account limiter + N hatalı TOTP'ta kilit + audit.
  Kurtarma kodu yok; cihaz kaybında `opadmin reset-mfa`.
- **Rota/host:** `/operator/login`, `/operator/login/totp`, `/operator/enroll`, `/operator/logout`,
  `/operator` (tenant listesi), `/operator/tenants/{id}`, `/operator/legal`, `/operator/billing`,
  `/operator/plaques`, `/operator/audit`. Zincir: hostGate → floodGate → sameOriginGate (operatör
  origin'i) → requireOperator → sessionGate. Oturumsuz → 303 login; bilinmeyen rota 404; DSN yoksa
  503 + adı konmuş fault. `TAPPA_OPERATOR_HOST` host kapısı (`taptime.mt/operator` 404). ops ve ana
  host AYNI SITE → SameSite yetmez, `sameOriginGate` şart (T38/M8-04 dersleri).
- **DB (§4.5 bilerek aşılıyor — tek yer `op_*`):** `tappa_operator` (LOGIN, NOBYPASSRLS, yalnız
  `op_*` EXECUTE + `platform_*` dar sütun yetkileri, `platform_admins` INSERT YOK) ·
  `tappa_opdefiner` (NOLOGIN, BYPASSRLS, `op_*` sahibi, SÜTUN düzeyi grant — `aes_key_ref`,
  `app_key_ref`, `password_hash`, `token_hash`, `code_hash` ASLA). `tappa_app` hiçbir yeni yetki
  almaz; `platform_*` üzerinde açık `REVOKE ALL` (T46: prod default privileges geniş); `op_*`
  üzerinde EXECUTE yok (`REVOKE ALL … FROM PUBLIC`). Elenenler: BYPASSRLS havuzu (`pool.go:303-311`
  prod'da reddeder, tek eksik `WHERE` her şeyi açar) · operatörün `WithTenant` döngüsü (liste
  çıkarılamaz, yasak deseni normalleştirir). Roller `scripts/db-init/01-roles.sql` + canlı küme
  için tek seferlik runbook (R5b migration'daki BYPASSRLS'i yakalar; `01-roles.sql` tarama dışı).
- **`op_*` sözleşmesi:** (i) ilk parametre operatör oturum token hash'i — fonksiyon canlı+MFA'lı
  oturumu KENDİSİ çözer, aktörü türetir (beyan edilemez); (ii) sabit sütun listesi, `SELECT *` yok;
  (iii) LIMIT ≤200; (iv) `SET search_path = pg_catalog, public`, dinamik SQL yok; (v) her çağrı
  `operator_audit_log`'a, değiştirenler ayrıca hedef tenant'ın `audit_log`'una AYNI transaction'da;
  (vi) hepsi tek migration + `db/queries/operator.sql` (belge) + elle yazılmış
  `internal/db/operator.go` (`resolve.go` emsali). Go: `db.OperatorDB` — `WithTenant` metodu YOK;
  müşteri paneli operatör paketini import etmez (testle).
- **Audit:** `operator_audit_log` (tenant_id YOK — ADR 0016 benzeri muafiyet, R5 WARN; append-only:
  REVOKE + `tappa_forbid_mutation` trigger) HER eylemi tutar — başarılı/başarısız girişler,
  enrollment, yayınlar ve tenant verisinin OKUNMASI (liste + detay; "sessiz tenant-ötesi okuma
  yok"). Tenant'ı değiştirenler tenant'ın `audit_log`'una da (`actor_id` = platform admin id,
  polimorfik/FK'sız — migration gerekmez; `detail.actor_kind="operator"`). Detail'e TOTP sırrı,
  token, parola ASLA.
- **Askı (ADR 0025):** `tenants.suspended_at`, `suspension_reason` (tappa_app SELECT evet, UPDATE
  hayır). Bloke: `ProtectWriting` altındaki bütün yazmalar (encode dahil) → 403 + gerekçe + ret
  audit'i; istisna parola değiştirme + çıkış. Bloke ETMEZ: NFC/QR tap'ler (§4.6 — karar motoruna
  dokunulmaz; guardrail eklemek ADR 0004 değişikliği ister), okumalar, CSV (GDPR taşınabilirlik +
  işverenin yasal kayıt yükümlülüğü), giriş, aktivasyon. Sıcak yol: `TouchAdminSession`
  `suspended_at`'i döndürür → `AdminIdentity.Suspended` → requireAdmin sonrası `suspensionGate`,
  ek sorgu yok. Sert kilit ayrı eylem (`op_disable_tenant_admins`).
- **Bootstrap:** `cmd/opadmin` filtre CLI (`cmd/rotatekek` emsali — DSN yok, sürücü yok, SQL üretir,
  operatör `psql` ile `tappa_owner` olarak uygular): `create --email --name` (pending satır +
  enrollment token hash'i; ham token stderr'e BİR KEZ, TTL 30 dk, SQL çıktısında ASLA) ·
  `reset-mfa` · `disable`. `platform_admins`'e yalnız `tappa_owner` INSERT → HTTP'den ya da app
  rolünden operatör yaratmak imkânsız; tablo boşsa kimse giremez. Reddedilen: env'den tohumlama,
  herkese açık kurulum sayfası.
- **Legal taşıma:** `/operator/legal` → `op_publish_legal(session, slug, body)`; `REVOKE INSERT ON
  legal_documents FROM tappa_app` (ADR 0016 §2b "yazma tarafında DB derinliği yok" borcu kapanır);
  `published_by` = `platform_admins.id` (eski satırlar "tenant admin (legacy)"); geri alma = yeni
  satır; tek replika → yayından sonra `legal.Store.Refresh`. Allow-list mekanizması tamamen
  kaldırılır: `TabLegal`, `OperatorOnly`, `PanelChrome.Operator`, `mayPublishLegal`,
  `config.OperatorAdminIDs`, `TAPPA_OPERATOR_ADMIN_IDS` (`05-config.yaml`, `.env.example`, README).
  KF hesabı sıradan müşteri olur.
- **Yapılmayacak:** müşteri adına giriş (impersonation). Gerekirse ileride tenant onaylı, süreli,
  salt-okuma destek erişimi (ayrı ADR).

### Görevler — A1 temel
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| OP-4 | ADR 0020 (operatör kimliği; ADR 0016 §5 + M7-06 "yeni giriş yok" + B kararının yerine geçer) + ADR 0021 (`op_*` arayüzü, elenenler) | S–M | orkestratör (+güvenlik okuması) | ADR'ler kabul; ADR 0016 durumu "kısmen yerine geçildi" | D-B |
| OP-5 | Migration: `platform_admins`, `platform_sessions`, `operator_audit_log` + `01-roles.sql` + runbook | M | tappa-db-migrator | tappa_app dört fiilde yetkisiz (prod default-priv simülasyonu dahil); tappa_operator `platform_admins` INSERT yok; `operator_audit_log` UPDATE/DELETE tappa_owner için bile hata; `RoleFacts.Privileged()=false`; active⇒parola+TOTP; R5 WARN'lar; Down temiz | OP-4 |
| OP-6 | `internal/operatorauth` (parola, TOTP, oturum, çerez, token, limiter/kilit) | L | builder + güvenlik | RFC 6238 Ek B vektörleri; aynı kod N goroutine → tam 1 başarı (-race); yanlış KEK açamaz; sızıntı testleri harici pakette; 8 s / 30 dk / MFA'sız / revoked / disabled hepsi red (saat enjekte DB testleri) | OP-5 |
| OP-7 | `OperatorDB` + config (`TAPPA_OPERATOR_DATABASE_URL`, `TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_HOST`) | M | builder | DSN yoksa /operator 503, panel etkilenmez; prod'da ayrıcalıklı rolle boot reddi; reflection: `WithTenant` yok; OperatorDB yalnız operatör handler'larına; TOTP KEK diğer anahtarlarla aynıysa başlangıç reddi | OP-5 |
| OP-8 | `/operator` handler'ları + UI (tappa-brand: restoran paneliyle karıştırılamayan "TAPTIME OPERATOR" kabuğu; tenant-ötesi her ekran girdiği tenant'ı başlıkta ADIYLA gösterir) | L | builder + tappa-brand + güvenlik | çapraz çerez: admin/çalışan çerezi operatör çerez adına konunca 303 (ve tersi); yanlış host 404; cross-origin POST'ta resolver çağrısı 0; bilinmeyen e-posta = yanlış parola (gövde + bcrypt sayısı); TOTP tekrarı red; N hatada kilit + audit; her giriş `operator_audit_log`'da; CSP/no-store/nosniff | OP-6, OP-7 |
| OP-9 | `cmd/opadmin` + README + (K2/K4) ops Ingress/DNS | M | builder (+kullanıcı ops) | sürücü yok, owner DSN adı kaynakta yok; çıktı tek transaction; ham token stdout'ta yok; süresi geçen token red | OP-5 |
| OP-10 | Legal'i taşı + allow-list'i emekliye ayır | M | builder + tappa-db-migrator + güvenlik | `has_table_privilege(tappa_app,'legal_documents','INSERT')=false`; yayın sonrası `/legal/privacy` yeni metin; `TestLegalPublicPath_WritesNothing` + `TestLegalReader_CannotReachTheDatabase` yeşil; sürüm listesi yayımlayanı gösterir; geri alma = yeni satır; `rg 'OperatorAdminIDs\|OperatorOnly\|TabLegal\|mayPublishLegal\|TAPPA_OPERATOR_ADMIN_IDS'` kod+deploy'da 0 (ADR geçmişi hariç); müşteri panel taramasında operatör öğesi yok | OP-8, OP-9 |

> **Kart düzeltmesi (2026-09-26, OP-4 uygulaması sırasında — 1.–4. tur ve 3. tur eki).** Yazıldı:
> [ADR 0020](../adr/0020-platform-operatoru-ayri-kimlik.md) (kimlik) ve
> [ADR 0021](../adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md) (`op_*`); ADR 0016
> durumu *"kısmen yerine geçildi"* + tarihli güncelleme bloğu. Yukarıdaki "Tasarım özü"nden
> sapmalar aşağıda; normatif hâlleri ADR'lerde. **Ölçüm yöntemi:** dev Postgres 17.10,
> `tappa_owner` oturumu; kalıcı katalogda iz bırakabilecek her şey `BEGIN … ROLLBACK`
> içinde (her sondadan sonra kalan `zz_probe_*` nesne/rol 0). 2. turdan itibaren çağıran
> `SET SESSION AUTHORIZATION tappa_app` ile gerçek, üye olmayan, NOSUPERUSER bir rol;
> definer'lar NOSUPERUSER BYPASSRLS. Commit gerektiren bilet sondaları `BEGIN … ROLLBACK`
> ile ölçülemediği için oturuma özel geçici nesnelerle (`pg_temp`) koştu (madde 10, 23).
> Sonda **olmayan** maddeler *(metin okuması)* diye işaretli.
>
> **1. tur**
> 1. **`op_*` (iv) `SET search_path = pg_catalog, public` AÇIKTI** → `pg_catalog,
>    pg_temp` + `public.`-nitelenmiş adlar (altı çözümleyicinin altısının emsali).
>    Yolda yazılmamış `pg_temp` ilk aranır: çağıranın aynı adlı geçici tablosu
>    fonksiyonun okumasını **ve** audit `INSERT`'ini kendine çekti. Gerçek fiyatı (2. turda
>    NOSUPERUSER sahip + gerçek çağıranla yeniden ölçüldü): tek aşamalı bir yazma gerçek
>    tabloya commit edilirken audit'i çağıranın geçici tablosuna gidiyor — değişiklik 1,
>    gerçek audit 0 (ADR 0021 §2 iv).
> 2. **OP-10'un `has_table_privilege(tappa_app,'legal_documents','INSERT')=false`
>    kriteri BUGÜN DE yeşil** — grant sütun düzeyinde; `has_any_column_privilege` = `t`
>    ve `tappa_app` yazabiliyor. Kriter boş; doğrusu `has_any_column_privilege(…,
>    'INSERT')=false`. OP-5'in *"dört fiilde yetkisiz"*i aynı sözleşmeyle ölçülür:
>    SELECT/INSERT/UPDATE `has_any_column_privilege`, DELETE `has_table_privilege`.
> 3. **`tappa_app` `REVOKE ALL` kapsamı `platform_*` adıyla sınırlı değil** —
>    `operator_audit_log` da (2. turda: bilet tablosu ve diziler — madde 15). Yeni tablo
>    dev'de ve üretimde `tappa_app=arwd` ile, taze CI veritabanında `ar` ile doğar;
>    ikisinde de açık.
> 4. **`tappa_opdefiner`'ın "ASLA" listesi tenant tablolarının yedi sır sütunudur**
>    (katalogdaki hash/anahtar-referansı sütunlarının tamamı); adıyla istisna
>    **`platform_sessions.token_hash`**: (i) `WHERE token_hash = …` ister, Postgres
>    `WHERE`'deki sütun için sütun SELECT'i ister (yoksa `permission denied`). Görür,
>    döndürmez.
> 5. **(v) "her çağrı" = kabul edilen her çağrı.** EXCEPTION'la reddedilen çağrının
>    audit `INSERT`'i geri alınır (0 satır). Her `op_*` yazdığı için `VOLATILE` zorunlu
>    (`STABLE`'da `INSERT` çalışma anında hata). (Okumaların audit'i 2. turda iki
>    aşamalı oldu — madde 10.)
> 6. **`totp_last_step` `NOT NULL` + başlangıç değeri her gerçek adımdan küçük** —
>    `NULL` iken `WHERE totp_last_step < $2` 0 satır döndürüp **ilk** kodu reddediyor.
> 7. **(vi) "hepsi tek migration"** A2'nin ayrı görevleriyle (OP-10, OP-11, …)
>    çelişiyor → ADR 0021 bunu *"her `op_*`'ın tanımı + sahipliği + grant'ı aynı
>    migration'da"* diye okur. *(metin okuması)*
> 8. **`tappa_operator` NOLOGIN ve parolasız doğar** — plan *"LOGIN"* diyor. Giriş,
>    parolayı Secret'tan veren ayrı adımda açılır; `01-roles.sql`'in ölçülmüş fail-open
>    düzeltmesinin (`tappa_app`) aynısı. *(metin okuması: `01-roles.sql` başlığı)*
> 9. **`operator_audit_log`'a tablo-boşaltma trigger'ı da** — plan *"REVOKE +
>    `tappa_forbid_mutation` trigger"* diyor; 00021 emsali: bugün
>    `tappa_forbid_mutation` kullanan her tabloda satır ve tablo-boşaltma trigger'ı
>    birlikte (katalogda ölçüldü).
>
> **2. tur (tappa-security-auditor RED; üçüncü göz ONAY, ORTA/DÜŞÜK bulgular)**
> 10. 🔴 **Okumalar İKİ AŞAMALI (bloklayan bulgu).** Tek aşamalı okuma izsizdi:
>     `SAVEPOINT` → `op_*` → `ROLLBACK TO` = veri elde, operatör log'unda 0 satır (denetçi
>     ölçtü). Karar: `op_begin_read` (oturum çözer, audit yazar, bilet döndürür — kendi
>     transaction'ında **commit**) → `op_read_*` (bileti doğrular, aynı `UPDATE`'te
>     tüketir, veriyi döndürür). 🔴 **Brief'in önerdiği `xmin = pg_current_xact_id()`
>     kontrolü AŞILIYOR — ölçüldü:** savepoint içinde yaratılan biletin `xmin`'i bir
>     alt-transaction kimliği, kontrol veriyi verdi. Yerine: bilet satırı üst düzey
>     kimliği kaydeder (`created_xact xid8 DEFAULT pg_current_xact_id()` — savepoint
>     içinde de üst düzeyi döndürüyor, ölçüldü) ve `pg_xact_status(created_xact) =
>     'committed'` şartı. **Ölçüm:** aynı transaction'da (üst düzey) ret · aynı
>     transaction'da (savepoint) ret · commit sonrası başka transaction'da veri · okuma
>     transaction'ı geri alınınca audit satırı kalıcı · commit'siz satır eşzamanlı başka
>     oturumdan görünmez (0; kendi oturumu 1) · tüketilip commit edilmiş bilet ret.
>     (Commit gerektiren kısım `BEGIN … ROLLBACK` ile ölçülemez: oturuma özel geçici
>     nesnelerle — `pg_temp`, bağlantıyla silinir — koştu; eşzamanlılık sondası
>     `legal_documents`'a `BEGIN … ROLLBACK` içinde bir satırla.) **Yeni sınırlar:**
>     geri alınan bir okuma bileti tüketilmemiş hâle döndürür ve süresi içinde yeniden
>     kullandırır (ölçüldü; yeni audit satırı yok, ama commit edilmiş satır aynı oturumu,
>     türü ve hedefi adlandırıyor) · başarısız oturum sondaları DSN sahibince gizlenebilir
>     · tek aşamalı bir yazma tenant verisi **döndüremez** (yoksa aynı izsiz okuma açılır).
>     Commit'ten bağımsız ikinci iz (`log_statement='all'` + `log_parameter_max_length=0`,
>     ikisi de superuser ayarı — ölçüldü; ya da pgaudit) karar verilmedi. *"`tappa_owner`
>     bile silemez"* düzeltildi: superuser trigger'ı kapatabilir ya da tabloyu `DROP`
>     edebilir (00005:22-23, 00021:79-82 — *"defence in depth … not an absolute"*).
> 11. **`op_record_auth_event`** — oturum öncesi başarısızlıklar için tek, dar, oturumsuz
>     definer: kapalı beş tür (`login_failed`, `unknown_email`, `totp_failed`, `locked`,
>     `enrollment_failed`), yalnız başarısızlık, aktör iddiası yok, **adres de adresin
>     hash'i de saklanmaz** (eşleşen hesabın id'si *hedef hesap*), `RETURNS void`.
>     `tappa_operator` `operator_audit_log`'a doğrudan `INSERT` tutmaz.
> 12. **Touch + oturum yüklemi tek definer'da (`op_touch_session`).**
>     `has_column_privilege('tappa_operator','platform_sessions','token_hash','SELECT')
>     = false` katalog kabulüdür (denetçi: touch için `SELECT (id, token_hash)` alan rol
>     bütün canlı hash'leri listeledi). OP-6'nın *"saat enjekte DB testleri"* kabulü,
>     yüklem veritabanında koştuğu için satırın zaman damgalarını geriye yazarak
>     karşılanır.
> 13. **Ters katalog testleri:** `public`'teki her `op\_%` fonksiyonunun sahibi
>     `tappa_opdefiner` · her `prosecdef` fonksiyonun sahibi {`tappa_resolver`,
>     `tappa_opdefiner`} içinde ve `rolsuper=f` (bugün altı fonksiyonun altısı
>     `tappa_resolver` — ölçüldü, test ilk gün yeşil) · `tappa_opdefiner`'ın üyesi 0.
> 14. **Geçici tablo gölgesi testi, çağıranın tabloyu definer'a `GRANT` ettiği adımı
>     ŞART koşar** — yeniden ölçüldü: GRANT'sız bozuk `search_path` `permission denied`
>     ile "reddedilmiş" görünür; GRANT'lı `forged-by-caller` okur.
> 15. **Diziler:** yeni tablolar **uuid** birincil anahtar kullanır; dizi doğarsa
>     `REVOKE ALL ON SEQUENCE … FROM tappa_app` (dizi varsayılanı `tappa_app=rU` —
>     ölçüldü; tablo `REVOKE`'u diziyi kapsamaz).
> 16. **Reddedilen `op_*` çağrısının audit kararı OP-11'den OP-10'a** (ilk `op_*`
>     orada sevk ediliyor). *(3. turda OP-5'e çekildi — madde 30.)*
> 17. **Enrollment:** tek ifadelik koşullu tüketim (ADR 0015 emsali); token 256 bit,
>     DB'de **anahtarsız** SHA-256; CLI sunucu anahtarı taşımaz.
> 18. **TOTP zarfı:** `internal/sun`'da yeni genel `Seal`/`Open` (AES-256-GCM, rastgele
>     nonce); AAD = `platform_admins.id`'nin 16 baytı (kırpılmaz); sır 160 bit. `Wrap`'a
>     sığdırma kolu kaldırıldı (`keys.go:98-103` 7/16 bayt dışını reddediyor). *"Yalnız
>     `internal/sun`"* iddiası **üretim kodu** için doğru; test dosyaları da `crypto/aes`
>     / `crypto/cipher` import ediyor.
> 19. **`TAPPA_OPERATOR_TOKEN_HMAC_KEY`** — operatör oturum token'ının HMAC'i kendi
>     değişkeni (etiketle türetme değil — `internal/adminauth/token.go`'nun kendi
>     uyarısı); diğer anahtarlardan farklılığı açılışta zorunlu; manifest testi;
>     `tappa-secrets` listesine.
> 20. **OP-10'un `rg` kriteri karşılanamaz** → *"`db/migrations` ve ADR geçmişi hariç"*:
>     `db/migrations/00020_create_legal_documents.sql:96-97` iki adı taşıyor ve uygulanmış
>     migration değişmez.
> 21. **Miras kalan yanlış atıf:** 72 bayt tavanı ADR 0014'te değil,
>     `internal/adminauth/password.go:132`'de (`MaxPasswordBytes = 72`). ADR 0020
>     düzeltildi; aynı hata **ADR 0019:47**'de duruyor — bu görev ADR 0019'a dokunmaz.
>     *(metin okuması)*
> 22. Alıntı sadakati düzeltildi: ADR 0016 §2 alıntısına *"Tappa tenant'ında"*; M9-08
>     kabul 2'nin aslı *"sessiz bir çapraz-tenant okuma yok"*; kabul 4 hücresi özet
>     olarak, tırnaksız. *(metin okuması)*
>
> **3. tur (tappa-security-auditor RED: bilet süresinin saati; üçüncü göz ONAY — 1. turun
> 11 bulgusu kapandı, bilet ölçümleri yeniden üretildi)**
> 23. 🔴 **Süreler `clock_timestamp()` ile; `now()` `op_*` içinde YASAK (bloklayan
>     bulgu).** Deponun deyimi `now()` (`tags.sql:584`, `invites.sql:232,271`,
>     `sessions.sql:56`) transaction başlangıcında donar ve bu tehdit modelinde transaction
>     sınırlarını DSN sahibi seçer. Denetçinin ölçümü burada yeniden üretildi (oturuma özel
>     geçici nesnelerle): ömrü 3 sn'lik bilet commit edildi, okuma transaction'ı 4 sn açık
>     tutuldu (`now()` 08:28:35.228'de kaldı, duvar saati 08:28:39.234) →
>     `clock_timestamp()` varyantı **ret**; `now()` varyantı savepoint geri almalarıyla
>     **3/3**, tek bir `DO` bloğunun istisna alt-transaction'larıyla **5/5 veri**; audit 1,
>     tüketilmiş bilet 0. `idle_in_transaction_session_timeout`, `transaction_timeout`,
>     `statement_timeout` üçü de `context = user` (ölçüldü) — çare değil. Kural bilet ömrü,
>     oturumun mutlak/boşta süresi ve enrollment token süresi için. **Bilet ömrü ≤ 60 sn**
>     (şema CHECK'i, ADR 0015 emsali). **`pg_xact_status` eski xid için `NULL`** döndürüyor
>     (ölçüldü: `'100'`, `'3'` → `NULL`); `IF … <> 'committed'` fail-open, bu yüzden koşul
>     tüketen `UPDATE`'in `WHERE`'inde **pozitif**. Sınır 4 (yeniden kullanım) ≤ 60 sn'ye
>     indi; ADR 0020'nin *"tek kullanımlık bilet"* ifadesi ona bağlandı.
> 24. **Bilet tablosu:** `tappa_operator` **dört fiilde de** yetkisiz (denetçi: `INSERT`
>     tutan rol audit'siz bilet basıp veri aldı; `UPDATE (consumed_at)` tutan rol tüketimi
>     sıfırladı); `tappa_opdefiner`'ın `UPDATE`'i yalnız `consumed_at`'te; `op_read_*`
>     **ham** bileti alıp içeride hash'ler; bilet okumanın **bütün** parametrelerini bağlar
>     (sayfa, imleç, arama terimi — 4. turda terim bilet hash'inin **içine** alındı ve
>     audit'ten çıkarıldı, madde 41); `audit_id NOT NULL REFERENCES
>     operator_audit_log`. `tappa_opdefiner`'ın `platform_admins` `INSERT`'i yok (pinli).
> 25. **Yazmalar yalnız `void` ya da `uuid` döndürür**; önceden var olan duruma bağlı dönüş
>     ya da hata ayrımı yok (denetçi: `SAVEPOINT` + `disable_admins('B')` → 3 +
>     `ROLLBACK TO` → audit yok = durum kehaneti). Katalog pini: `op_read_%`,
>     `op_begin_read`, `op_touch_session` dışındaki `op_*` için `proretset = f` ve dönüş
>     tipi `void`/`uuid`; *"katalogdan okunamaz"* iddiası kısmi pine yumuşatıldı.
> 26. **`op_record_auth_event` bir internet yazma kapısıdır:** oturum öncesi satırlar
>     IP'den bağımsız, süreç geneli bir tavanla sınırlı (sayısı OP-6/OP-8); limiter'ın
>     reddettiği istek satır yazmaz; **TOTP kilidi bu satırlardan türetilmez**, hesabın
>     kendi sayacından gelir. Gerekçe `adminlogin.go:1232` ve `:1300-1301`.
> 27. **`op_open_session` — ikinci oturumsuz istisna:** oturumu doğurur ve köken satırını
>     (`login`) **aynı ifadede** yazar — 3. tur ekinden beri enrollment kökenini
>     `op_complete_enrollment` yazar; yeni güç değil (sınır 1), yalnız iz.
>     Çıkış `op_close_session` (çıkış satırı aynı ifadede). `tappa_operator`
>     `platform_sessions`'ta hiçbir fiil tutmaz. Enrollment tüketimi `tappa_operator`'ın
>     ifadesi kaldıkça **yeni sınır 10** (kimlik bilgisi yeniden yazımı, iz'siz); kapatan
>     öneri ve **benimsenirse değişecek kurallar** (oturumsuz istisna kümesi, *"digest/zarf
>     okumaz"*, `prosecdef` sahip kümesi) ADR 0021 §1 sonunda. *(Sınır 10 3. tur ekinde
>     kapandı — madde 33.)*
> 28. **Audit satırının zorunlu içeriği:** oturum kimliği, tür, hedef (sütun adları OP-5) —
>     sınır 1 ve 4'ün savunusu buna dayanır.
> 29. **Asla loglanmayanlar:** okuma bileti, TOTP kodu, enrollment token'ı — `slog`, hata
>     mesajı, audit `detail`'i (CLAUDE.md §7'de yoklar → açık iş).
> 30. **Reddedilen çağrının audit kararı OP-10'dan OP-5'e** (madde 16'nın yerine geçer):
>     `op_touch_session` ve `op_record_auth_event` OP-5'te doğuyor; reddedilebilen ilk
>     çağrı OP-8'in oturum kapısı.
> 31. **`pending` hesap** giriş formunda *"aynı yanıt, aynı süre"* kümesinde (yoksa
>     *"bekleyen operatör"* kehaneti).
> 32. **Atıflar ve metin** *(metin okuması)*: `token.go` yer tutucu gerekçesi `:59-62`'ye
>     göre (iki sızıntı-test takımı birbirine kefil olmasın), bağımsızlık uyarısı
>     `:82-87`; ADR 0020 §7'nin ölçümü `has_column_privilege` ile adlandı; *"sahte bir
>     başarı basamaz"* yalnız `op_record_auth_event` için; ADR 0020'nin yerine geçilen
>     tablosuna ADR 0016 bloğunun saydıkları eklendi (*".env"*, *"kim yayımladı"*,
>     *"audit'in yeri"*); risk 3'e `TAPPA_OPERATOR_TOKEN_HMAC_KEY`. ⚠️ **00005 atfı 22-23
>     olarak KALDI:** brief *"21-22"* diyordu; `grep -n` alıntılanan iki satırı
>     (*"…superuser trigger'i"* / *"DISABLE edebilir; bu bilincli defense-in-depth, mutlak
>     degil."*) 22 ve 23'te gösteriyor (orkestratör 3. tur ekinde onayladı).
>
> **3. tur eki (orkestratör kararı: öneri benimsendi, sınır 10 kapandı)**
> 33. **`op_complete_enrollment` — üçüncü oturumsuz istisna.** Tek koşullu ifadede: ham
>     enrollment token'ını içeride hash'ler; *id + hash*, `pending`, kullanılmamış ve
>     süresi geçmemiş (`clock_timestamp()`) koşullarıyla tüketir; parola digest'ini,
>     mühürlü TOTP sırrını ve `totp_last_step`'in ilk değerini yazar; durumu `active`
>     yapar; oturumu açar ve köken satırını (`enrollment`) yazar. İlk kodun doğrulaması
>     ve adımın bulunması Go'da; adımın yazılması enrollment kodunun ilk girişte tekrar
>     oynatılmasını engeller. Zarfın AAD'si hesap id'si olduğu için `opadmin create` id'yi
>     kendisi üretip linke koyar (arama definer'ı dördüncü ad olurdu).
> 34. **`tappa_operator` `platform_admins`'e HİÇBİR ŞEY yazamaz** (`INSERT`, `UPDATE`,
>     `DELETE`); katalog: `has_any_column_privilege(…,'UPDATE')` ve `'INSERT'` = `false`,
>     `has_table_privilege(…,'DELETE')` = `false`, enrollment hash'inde `SELECT` `false`.
>     **TOTP adımı `op_open_session`'ın içinde** ilerler (tekrar koruması
>     `totp_last_step < $adım` orada; oturum o güncellemenin döndürdüğü satırdan doğar —
>     tekrar edilen kod oturum açamaz); son giriş ve sayaç sıfırlama da orada. **Kilit
>     sayacı** `op_record_auth_event`'in `totp_failed` satırıyla aynı ifadede artar (üç
>     adlı kümeye dördüncü ad eklememek için); kilit kararı satırları saymaz, sayacı okur.
>     ⚠️ Madde 26'nın *"TOTP kilidi bu satırlardan türetilmez, hesabın kendi sayacından
>     gelir"* cümlesi ayakta, ama *"sahte satır kilide dönüşmez"* diye okunamaz:
>     internetten gelen için doğru, **DSN sahibi için değil** — sayaca giden her yol onun
>     çağırabildiği bir definer'dır (ADR 0021 sınır 7).
> 35. **Giriş araması öneri olarak kalır;** `tappa_operator` digest'i ve zarfı `SELECT`
>     eder. Yeni **sınır 11:** DSN sahibi bütün operatörlerin digest'lerini okuyup
>     çevrimdışı kırmayı deneyebilir (bcrypt cost 12, ≥14 rune); mühürlü sırları okur ama
>     `TAPPA_OPERATOR_TOTP_KEK` olmadan açamaz (aynı süreçteki RCE KEK'i de alır — K9).
>
> **4. tur (iki mercek 3. turda ONAY; ORTA/DÜŞÜK bulgular — çoğu denetçilerin kendi
> sondasıyla ölçülmüş; O-1…O-4 bu turda `BEGIN … ROLLBACK` / oturum-geçici `pg_temp` ile
> yeniden üretildi)**
> 36. **O-1 · donan saatlerin hepsi yasak** — `now()`, `CURRENT_TIMESTAMP`,
>     `transaction_timestamp()`, `statement_timestamp()`, `LOCALTIMESTAMP`, `LOCALTIME`,
>     `CURRENT_TIME`, `CURRENT_DATE`. Ölçüldü: uyku tek bir `DO`'nun **içindeyken**
>     `statement_timestamp()` süresi dolmuş bileti **5/5**, `clock_timestamp()` **0/5**
>     verdi. `DO` testi uykuyu `DO`'nun içine koyar; `tappa_opdefiner`'ın fonksiyonlarının
>     `prosrc`'unda ve bu tabloların zaman DEFAULT'larında (`pg_attrdef`) katalog taraması.
>     Çerçeve düzeltildi: kural yeni değil — `transactions.sql:163-164` ve ADR 0006
>     (:109-113, :189) zaten `clock_timestamp()`'i seçmiş (B8).
> 37. **O-2 · bilet INSERT sütunları** — `created_at`, `created_xact`, `consumed_at`
>     `tappa_opdefiner`'ın INSERT listesinde YOK, DEFAULT doldurur (ADR 0015 §2 emsali);
>     `has_column_privilege(…,'created_xact'|'created_at'|'consumed_at','INSERT') = false`;
>     şemada `CHECK (created_xact > '2'::xid8)`. Ölçüldü: `pg_xact_status('1')` ve `('2')`
>     = `committed` — ADR 0021 §2 v(4)'ün *"NULL fail-closed"* savunması bununla düzeltildi.
> 38. **O-3 · TOTP adımı duvar saatine bağlı** (`op_open_session` ve
>     `op_complete_enrollment`): `p_step BETWEEN cur-1 AND cur+1`. Ölçüldü: zehir
>     (`bigint` üst sınırı), `cur±2`, `cur`+1 yıl → hayır; `cur-1`, `cur`, `cur+1` → evet.
>     Sınır 7 düzeltildi (sayaç için DB yolu yok, adım için var); yeni sınır 13 (Go/DB saat
>     ayrışması, fail-closed).
> 39. **O-4 · yazılan zaman damgaları `clock_timestamp()`** — `operator_audit_log`'un zaman
>     sütunu DEFAULT ve INSERT listesinde yok; K6 tenant `audit_log.at` açıkça
>     `clock_timestamp()`. Ölçüldü: 2 sn açık transaction'da `DEFAULT now()` 2,007 sn geri,
>     `clock_timestamp()` 0,001 sn. Audit–okuma farkı ≤ 60 sn.
> 40. **B1 · "asla" = `SELECT` yasağı** — `op_complete_enrollment`'ın digest/zarf
>     `UPDATE`'i §3.3'te `token_hash`'in yanında adlı yazma istisnası; katalog testi yetki
>     türünü `'SELECT'` diye söyler; ayrım OP-18'in `aes_key_ref`/`app_key_ref` yazımında
>     yük taşıyacak.
> 41. **B7 · arama terimi** — audit'e **yazılmaz** (ne ham ne hash'i). Brief'in önerdiği
>     "bilet satırında terimin anahtarsız hash'i" yerine **daha iyisi ölçülüp seçildi**:
>     bilet hash'i `sha256(ham bilet ‖ parametrelerin kanonik jsonb metni)`; ham bilet
>     256 bit ve saklanmaz, yani saklanan hiçbir değer terimden türetilemez (ölçüldü: başka
>     terim 0, başka sayfa 0, terimin tek başına hash'i 0 eşleşme; jsonb metni anahtar
>     sırasından bağımsız). *"Sonuç sayısı sınıfı"* audit'e giremez — audit satırı sorgudan
>     **önce** commit edilir.
> 42. **D-1 · kilit koşulu `op_open_session`'ın AYNI `UPDATE`'inde** (eşik ve pencere
>     `clock_timestamp()` ile). **D-2 · kapsam:** *"yazmanın hatası önceki durumdan
>     bağımsız"* kuralı tenant durumunu değiştiren yazmalar içindir. **`tappa_operator`
>     `operator_audit_log`'u `SELECT` edemez** — görüntüleyici iki aşamalı `op_read_audit`
>     (OP-14). **B14 · var olmayan hedef** → tenant satırı `INSERT … WHERE EXISTS`, aynı
>     `void`, yalnız operatör satırı (davranış testi; kehanet kapandı).
> 43. **D-3 · enrollment:** kimliksiz son adım bcrypt + `Seal` öder → oran sınırı (yeni
>     sınır 12, ADR 0015 emsali); "düz sır süreç belleğinde" seçeneği kimliksiz bir GET'le
>     bellek ayırma ilkeline dönüşebilir (açık maddede); **token sorgu dizgisinde
>     taşınmaz** (ingress log'u — `requestlog.go:399-406`), fragment ya da yol parçası, OP-9
>     ölçer; ingress "asla loglanmaz" listesinde. **D-4:** aynı token, N eşzamanlı çağrı →
>     tam 1 başarı (emsal `internal/db/invites_test.go:213`). **B9:** `reset-mfa` tanımlandı
>     (zarfı siler, sayacı sıfırlar, `pending`, oturumları iptal eder, yeni token + id'li
>     link) — OP-9 açık işi. **B11/B12/B4/B5/B6** metin düzeltmeleri (*"Yapabildikleri"*;
>     `tappa-secrets` listesi iki ADR'de aynı, rol parolası dahil; *"kilidin yeri"* açık
>     maddeden çıktı; *"üç dar istisna"*; madde 27'de `op_open_session` yalnız `login`).
>
> **Kabullere bağlananlar — kart kart** (2. turun paragrafının yerine geçer; 4. turda
> güncellendi):
> - **OP-5:** tablolar (bilet tablosu dahil; hepsi uuid PK, dizi yok) + `01-roles.sql` +
>   runbook. **Burada doğar:** `op_touch_session`, `op_record_auth_event`,
>   `op_open_session`, `op_complete_enrollment`, `op_close_session` — hepsi audit yazar ve
>   hepsi **tek aşamalı yazmadır** (testler her yeni `op_*` için yeniden koşar). **Testler:**
>   - katalog **ileri yön** (`proconfig`, PUBLIC ve `tappa_app` `EXECUTE`, ilk argüman ve
>     üç oturumsuz ad, dönüş tipi pini) · **donan saat taraması** (`prosrc` + `pg_attrdef`)
>     · **ters yön** (her `op\_%`'ın sahibi `tappa_opdefiner`; her `prosecdef`'in sahibi
>     {`tappa_resolver`, `tappa_opdefiner`} ve `rolsuper=f`; `tappa_opdefiner`'ın üyesi 0);
>   - rol/tablo katalog: `tappa_operator` bilet tablosunda ve `platform_sessions`'ta dört
>     fiilde yetkisiz, `token_hash` `SELECT` `false`, `operator_audit_log`'da `INSERT` ve
>     `SELECT` yok, `platform_admins`'te hiçbir yazma yok; `tappa_opdefiner`
>     `platform_admins` `INSERT` yok, digest ve zarfta `SELECT` yok, bilet `UPDATE`'i yalnız
>     `consumed_at`, bilet `created_xact`/`created_at`/`consumed_at` `INSERT` yok, audit
>     zaman sütunu `INSERT` yok;
>   - **geçici tablo gölgesi testi `GRANT` adımıyla** · **dönüş pini** (her yeni `op_*`
>     için yeniden);
>   - enrollment: duvar saatiyle dolmuş / kullanılmış / id-hash eşleşmeyen token ret, aynı
>     hatayla; `active` hesaba ikinci enrollment ret; zehirli ilk adım ret; aynı adımla
>     ardından gelen `op_open_session` ret; **aynı token, N eşzamanlı çağrı → tam 1
>     başarı**;
>   - TOTP adımı: aynı adımla ikinci `op_open_session` ret (oturum ve köken satırı yok);
>     **zehirli adım** ret ve hesap kilitlenmez, `cur±1` kabul; **`pending` ya da
>     `disabled` hesapla `op_open_session` → EXCEPTION**; kilit koşulu sağlanmamış hesap →
>     EXCEPTION (aynı `UPDATE`);
>   - oturum: mutlak ve boşta süresi dolmuş (duvar saatiyle), MFA'sız, iptal edilmiş,
>     `disabled` → exception + 0 audit;
>   - kilit ve sınır 7 **eşitlendi (B3):** sayaç yalnız `totp_failed` satırıyla artar ve
>     başarıda sıfırlanır; **parolayı bilmeyen bir saldırgan kilitleyemez** (satır ancak
>     parola adımından sonra ve limiter'ın izniyle doğar; parolayı bilen biri tasarım gereği
>     kilitleyebilir), **DSN sahibi kilitleyebilir** (sayılı,
>     sınır 7). Reddedilen çağrının audit kararı da burada.
> - **OP-6:** *"saat enjekte DB testleri"* → yüklem veritabanında duvar saatiyle koştuğu
>   için satırın zaman damgalarını geriye yazarak; kilit eşiği ve penceresi; zarf
>   `internal/sun`'ın yeni `Seal`/`Open`'ı; enrollment sırasında düz sırrın nerede
>   tutulduğu (bellek seçilirse kayıt sayısı ve ömrü tavanlı); **limiter testi (B3'ten
>   taşındı):** limiter'ın reddettiği istek satır yazmaz ve sayacı artırmaz.
> - **OP-7:** **dört** değişken — `TAPPA_OPERATOR_DATABASE_URL`, `TAPPA_OPERATOR_TOTP_KEK`,
>   `TAPPA_OPERATOR_HOST`, `TAPPA_OPERATOR_TOKEN_HMAC_KEY`; TOTP KEK'i ve token HMAC
>   anahtarı diğer her anahtardan farklı değilse açılış reddi. **OP-5'in eklediği kabuller
>   (2026-09-26, 2.–4. tur; gerekçeler OP-5 kart düzeltmesinde, madde 19 ve 33):**
>   (a) operatör havuzu `log_parameter_max_length_on_error = 0`'ı (gerekiyorsa
>   `log_parameter_max_length`'i de) bağlantı **başlangıç parametresi** olarak iğneler —
>   başlangıç paketi rol varsayılanını ezer, **OP-7 ölçer** — ve açılışta
>   `current_setting('log_parameter_max_length_on_error')` 0 değilse havuzu açmayı
>   **reddeder**; (b) operatör sorguları yalnız **bağlı parametreyle** yazılır, SQL metnine
>   değer gömülmez; (c) Go tarafı `PgError.Detail`'i asla log'lamaz; (d)
>   `db/queries/operator.sql` + `internal/db/operator.go` (ADR 0021 §2 vi).
> - **OP-8:** oturum kapısı `op_touch_session`; giriş sonu `op_open_session`, çıkış
>   `op_close_session`; **enrollment handler'ı `op_complete_enrollment`'a bağlı** (B10);
>   her okuma ekranı önce `op_begin_read`'i ayrı transaction'da commit eder; `pending`
>   hesap *"aynı yanıt, aynı süre"* kümesinde; `POST /operator/enroll` oran sınırı
>   (sınır 12); limiter testi (OP-6 ile).
> - **OP-9:** `opadmin create` hesabın id'sini kendisi üretir ve enrollment linkine
>   token'la birlikte koyar; **token sorgu dizgisinde taşınmaz** — fragment ya da yol
>   parçası, ingress'in ne log'ladığı ölçülür; **`reset-mfa`** (zarfı siler, sayacı
>   sıfırlar, `pending`, oturumları iptal eder, yeni token + id'li link) — açık iş, adıyla.
> - **OP-10** (ilk `op_read_*` = sürüm listesi; `op_publish_legal` `void` bir tek aşamalı
>   yazma): iki aşamalı okuma testleri (aynı transaction'da — üst düzey ve savepoint —
>   yaratılan bilet ret; commit sonrası veri; okuma transaction'ı geri alınınca audit
>   kalıcı; tüketilip commit edilmiş bilet ret; farklı parametreli bilet ret) · eşzamanlı
>   görünmezlik · bilet süresi (süresi dolmuş ret; açık tutulan transaction içinde dolunca
>   ret — savepoint ve **uykusu içinde** bir `DO` bloğu; `now()` ve `statement_timestamp()`
>   mutasyonları kırmızı) · bilet sahteciliği (`created_xact`/`created_at` INSERT'i `42501`,
>   `'1'`/`'2'` CHECK ile ret) · geçici tablo ve dönüş pinleri yeni fonksiyonlar için
>   yeniden. `rg` kriteri: *"`db/migrations` ve ADR geçmişi hariç"*.
> - **OP-11:** adlar `op_read_` önekiyle ve ikinci argümanda biletle (örn.
>   `op_read_tenants`, `op_read_tenant_detail` — kartın `op_list_tenants` /
>   `op_tenant_detail`'i yerine); *"her çağrı tam 1 operatör audit satırı"* → *"kabul edilen
>   her okuma `op_begin_read`'de tam 1 satır"*; **kemer davranış testi** (A için çağrılan
>   `op_read_*` B'nin satırını döndürmez); arama terimi audit'e yazılmaz, bilet hash'inde
>   bağlanır; iki aşamalı ve süre testleri her yeni `op_read_*` için yeniden.
> - **OP-14:** görüntüleyici iki aşamalı `op_read_audit`; `tappa_operator`'ın
>   `operator_audit_log` `SELECT`'i yok.
> - **OP-15/OP-16** (tenant'ı değiştiren ilk yazmalar): kemer testi yazma yönünde (A için
>   çağrılan yazma B'yi değiştirmez); dönüş `void`/`uuid`, durum ayrımı yok; **var olmayan
>   hedef** aynı `void`'u verir ve tenant satırı yazmaz (B14); K6 tenant `audit_log`
>   satırının `at`'ı `clock_timestamp()` ile yazılır — uykusu tek bir `DO` ifadesinin
>   içinde olan bir yazmada `at` duvar saatine ≤1 sn yakın (kapanış kontrolü Y4: `at`'ı
>   unutan bir `op_*`'ı katalog taramaları yakalamaz, DEFAULT `audit_log`'dadır).
>
> **Kapanış kontrolü (2026-09-26, orkestratör — ONAY'dan sonra, yalnız metin):** Y1 imleç
> audit'e yazılmaz, yalnız içeriksiz sayfa bilgisi (ADR 0021 §2 v 1, §3; ADR 0020 §5) ·
> Y2 ham bilet 256 bit karara bağlandı · Y3 `'now'`/`'today'` literalleri yasak listesinde,
> tarama kelime sınırıyla · Y4 yukarıdaki OP-15/16 testi · Y5 "parolayı bilmeyen saldırgan"
> · Y6 OP-5'te doğan beşi de tek aşamalı yazma.
>
> **Açık bırakılanlar:** tam liste **ADR 0020 ve ADR 0021'in *"Karar verilmedi"*
> bölümlerinde** (burada tekrarlanmaz). *(AES-256-GCM kodunun yeri 2026-09-26'da
> orkestratör kararıyla kapandı: mühür `internal/sun`'da kalır — 2. turda yeni
> `Seal`/`Open` ile —, TOTP'nin HMAC'i `internal/operatorauth`'ta; CLAUDE.md §3 değişmez —
> ADR 0020 §1.)* **Açık işler (bu görevin dışında):** ADR 0005'e risk eklemesi (Append
> kuralı; `cmd/tappa/adr0005_test.go` sayımları da değişir) · **CLAUDE.md güncellemesi** —
> §3 dizin haritası (`internal/operatorauth`, `cmd/opadmin`, `OperatorDB`), §4.5'in
> *"uygulama `tappa_app` rolüyle bağlanır"* cümlesi, §7'nin *"asla loglanmaz"* listesine
> okuma bileti, TOTP kodu ve enrollment token'ı. **OP-10 tuzağı:** izin listesini
> sabitleyen M7-06 testleri ADR 0016'da, `m7-portal.md`'de, bu dosyada ve kod
> yorumlarında adıyla anılıyor; silinirlerse `TestEveryNamedTestExists` kırılır. Yol
> kapalı değil: ya aynı adla tutulurlar ya da sarkan atıf envanterinin 2026-09-19 emsaliyle
> bütçe aynı değişiklikte, gerekçesiyle artırılır (ADR 0020 Sonuçlar).

> **Kart düzeltmesi (2026-09-26, OP-5 uygulaması sırasında).** Yazıldı: migration
> `db/migrations/00026_create_platform_operator.sql` (dört tablo + beş `op_*`),
> `scripts/db-init/01-roles.sql`'e işaretli **OPERATOR ROLES** bloğu, `deploy/README.md` →
> *"Operator roles (M10 OP-5) — tek seferlik kurulum"* runbook'u,
> `internal/db/operatorschema_test.go` + `internal/db/operatorfuncs_test.go`. Ölçüm: dev
> Postgres 17.10, `tappa_owner`; sondalar `BEGIN … ROLLBACK`, kimlik
> `SET LOCAL SESSION AUTHORIZATION` ile. Yukarıdaki OP-4 bloğundan ve ADR'lerden sapmalar ve
> ADR'lerin OP-5'e bıraktığı kararlar:
>
> 1. 🔴 **Deploy sırası — ölçüldü, çözüldü.** Roller canlı kümede yok (`01-roles.sql` yalnız
>    boş PGDATA'da koşar). 00026'nın ilk ifadesi rolleri, niteliklerini ve üyeliklerini
>    sınar ve eksikse runbook'u **mesajın içinde** adıyla göstererek düşer (goose yalnız
>    `PgError.Error()` = severity + message + SQLSTATE basar; HINT kaybolurdu). Dev'de,
>    roller yokken: `goose up` exit 1, `… needs the cluster role(s) tappa_opdefiner,
>    tappa_operator … (SQLSTATE 55000)`, `goose_db_version` 25 → 25. `deploy.yml`'den
>    okundu: Job `backoffLimit: 2` → Migrate adımı `failed >= 3`'te `exit 1` → *"Roll out
>    the server"* koşmaz → `20-app.yaml` uygulanmaz, **eski pod servis verir**, şema 25'te
>    kalır. Yani sıra ters olursa deploy kırmızı, ürün ayakta. **Sıra:** canlıda runbook →
>    sonra `main`'e birleştirme (runbook bunu başta söyler; kurtarma `workflow_dispatch`).
> 2. **Roller tek kaynaktan.** Blok `01-roles.sql`'de `>>> OPERATOR ROLES (M10 OP-5) >>>`
>    işaretleri arasında; runbook onu `sed` ile keser (elle kopya yok). İdempotent: `IF NOT
>    EXISTS` + koşulsuz `ALTER ROLE` (yanlış nitelikleri indirger; `tappa_operator`'ın
>    LOGIN'ine dokunmaz). Dev'de iki kez uygulandı: iki koşu da rc=0. `tappa_operator`
>    CONNECT + USAGE; `tappa_opdefiner` yalnız USAGE (NOLOGIN — `tappa_resolver`'ın şekli;
>    ADR §5'in "CONNECT/USAGE"si iki rol için birlikte yazılmıştı).
> 3. **Tablo ve sütun adları (ADR'ler OP-5'e bıraktı).** `platform_admins`,
>    `platform_sessions`, `operator_audit_log`, bilet tablosu **`operator_read_tickets`**.
>    Operatör eyleminin hedef tenant'ı **`target_tenant_id`** — `tenant_id` DEĞİL: o adla bir
>    sütun `redline` R5'i, `rlsforce_test.go`'yu ve `insertscope_test.go`'yu tabloyu tenant
>    verisi sayıp tenant-GUC politikası istemeye iterdi; FK de yok (00020'nin "varlık
>    kehaneti" gerekçesi, ADR 0021 B14). Audit sütunları: `kind` (kapalı küme: beş oturum
>    öncesi başarısızlık + `login`, `enrollment`, `logout`; sonraki her `op_*` kendi
>    migration'ında genişletir), `session_id`, `actor_admin_id`, `target_admin_id`,
>    `target_tenant_id`, `target_scope`, `page_number`, `page_size` (içeriksiz sayfa),
>    `detail` (`'{}'`), `at` (`DEFAULT clock_timestamp()`, hiçbir INSERT listesinde yok).
> 4. **Gönüllü RLS: ENABLE + FORCE, dördünde de; tek politika.** Gerekçe M8-02 FAZ E'nin
>    ölçümü: geri yükleme varsayılan ACL'i her `CREATE TABLE`'a yeniden uygular, REVOKE'u
>    yaymaz → `tappa_app` bu tablolarda SELECT/INSERT kazanırdı. RLS açık ve `tappa_app`
>    için politika yokken o artık 0 satır okur, yazamaz
>    (`TestOperator00026_RestoreResidueReadsNothing`). **FORCE da** — çünkü
>    `scripts/pg-restore-verify.sh` 2. bölüm ENABLE ve FORCE sayıları farklı olan geri
>    yüklenmiş veritabanını reddeder; ENABLE-yalnız bir tablo her felaket kurtarmasını
>    *"do not put this database into service"* ile bitirirdi (ilk taslak ENABLE-yalnızdı; bu
>    okumayla değişti). Bedeli migration başlığında: süper kullanıcı olmayan bir sahip
>    (yönetilen Postgres) `opadmin` yazımında FORCE'a takılır — o topoloji doğunca karar.
> 5. **`tappa_operator`'ın kesin listesi:** `platform_admins` üzerinde yalnız
>    `SELECT (id, email, display_name, status, password_hash, totp_secret_sealed,
>    totp_locked_until)` ve tek politika `FOR SELECT TO tappa_operator USING (status =
>    'active')`. Sonuç: sınır 11 **aktif** hesapların digest'lerine daralır; `pending` ve
>    `disabled` hesap giriş işleyicisine bilinmeyen adresten **yapısal olarak** ayırt
>    edilemez (ADR 0020 §3'ün "aynı yanıt" kümesi). Diğer üç tabloda dört fiilin hiçbiri.
>    `tappa_opdefiner`'ın sütun listeleri migration'da ve `TestOperator00026_PrivilegeMatrix`'te
>    tam liste olarak pinli.
> 6. **Kartın "beşi de audit yazar" cümlesi `op_touch_session` için YANLIŞ — düzeltildi.**
>    `op_touch_session` yazar (`last_used_at`) ama audit satırı **yazmaz**: her `op_*` onu
>    çağırır (§2 i), yani bir satır her kabul edilen eylemin arkasına iki satır koyar ve
>    OP-11'in *"kabul edilen her okuma tam 1 satır"* kabulünü kırar (`op_close_session` de onu
>    çağırıyor). Diğer dördü kabul edilen her çağrıda **tam bir** satır yazar (testlerde
>    sayılı). ADR 0021 Sonuçlar zaten *"ilk tek aşamalı yazmalar … `op_close_session` ile üç
>    oturumsuz fonksiyon"* diyordu. `op_touch_session` `OUT session_id, OUT admin_id` ile tek
>    satır döndürür (`proretset = f`; dönüş pininden adıyla muaf).
> 7. **`op_record_auth_event(p_kind, p_email DEFAULT NULL, p_admin DEFAULT NULL)` —
>    genişletme, adıyla.** Adres verilmezse hedef hesap **id** ile aranır: `totp_failed`,
>    `locked` ve `enrollment_failed` Go'nun bir adres değil bir id tuttuğu yerlerde doğar
>    (ara çerez, enrollment linki). Var olmayan bir id FK hatası DEĞİL (hedef `NULL` —
>    varlık kehaneti yok). Tür çağıranın iddiası, hedef veritabanının kendi araması.
> 8. **Ret biçimi:** her `op_*` her reddi için tek mesaj, **SQLSTATE 28000**; bu yollarda
>    başka hiçbir şey 28000 üretmez, yani eksik bir grant'ın 42501'i testte kabul yerine
>    geçemez. Kapalı küme dışı tür: 22023.
> 9. **Reddedilen `op_*` çağrısının audit kararı (ADR 0021 "Karar verilmedi"): Go ayrı bir
>    transaction'da satır YAZMAZ; `op_record_auth_event`'in kümesi büyümez.** Reddedilebilen
>    ilk çağrı OP-8'in oturum kapısıdır ve girdisi internetten gelen bir çerezdir: her ret
>    için bir satır, append-only bir tabloya kimliksiz, sınırsız bir yazma ilkeli olurdu
>    (`internal/handler/adminlogin.go:1300-1301`'in dersi) ve satırın bağlayacağı bir
>    kimlik yoktur (hash çözülmedi). Sınır 3 zaten DSN sahibine karşı bu izi değersiz
>    kılar. Saldırıya değen retlerin kendi türleri var (`login_failed`, `totp_failed`,
>    `locked`, `enrollment_failed` — Go'nun kararı, OP-8); oturum kapısının reddi süreç
>    log'una hash'siz yazılır.
> 10. **`op_record_auth_event` içinde ikinci (DB tarafı) tavan — ÖLÇÜLDÜ, KONMADI.** Doğal
>     biçimi (son 10 dk'da ≤ N oturum öncesi satır; satır ve sayaç birlikte) rolled-back bir
>     sondada koşuldu, N=20: 25 bilinmeyen-adres çağrısı → **20 yazıldı, 5 kırpıldı**;
>     ardından kurban hesaba 5 yanlış TOTP → **0 yazıldı, 5 kırpıldı**, sayaç **0** — parola
>     gerektirmeyen bir sel TOTP kilidini **kapatıyor**. Kilidi korumanın tek yolu
>     `totp_failed`'ı tavandan muaf tutmak, o zaman da DSN sahibinin `totp_failed` yazımı
>     sınırsız kalır, yani tavan varlık sebebini (DSN sahibini bağlamak) kaybeder. Sayılı
>     sınır olarak kalır (ADR 0021 sınır 7).
> 11. **Kilit sayıları GEÇİCİ: N = 5, pencere 15 dk.** Mekanizma OP-5'in (kilit
>     `op_open_session`'ın AYNI `UPDATE`'inde), sayılar OP-6/OP-8'in (ADR 0020); mekanizma
>     sayısız var olamadığı için konuldu. İki fonksiyonda geçer; uyumlarını
>     `TestOpOpenSession_TheLockIsTheAccountsCounterInTheSameUpdate` pinler (4 hata kilitlemez,
>     5 kilitler, pencere ~900 sn, pencere geçince kabul ve sıfırlama, audit satırları
>     kilitlemez). Değişiklik = yeni migration'da `CREATE OR REPLACE`.
> 12. **Enrollment token hash sözleşmesi (OP-9 için):** `encode(sha256(convert_to(token,
>     'UTF8')), 'hex')` — linkte taşınan **metnin** baytları üzerinde, küçük harf hex.
>     `cmd/opadmin` aynı baytları hash'lemeli. Şema: hash şekli CHECK; `enroll_issued_at`
>     sütunu eklendi (hash/issued/expires üçlüsü birlikte); süre **tavanı 1 sa** (ürün TTL'i
>     30 dk'nın üstünde — ADR 0015 emsali; tek saat okuması şartı olmasın);
>     `pending ⇒ token var`.
> 13. **Kimlik bilgisi şekli:** `password_hash` bcrypt, **cost 12–14** (taban ADR 0020 §1'in
>     cost'u, tavan 00018'inki — M7-03 A'nın cost-31 dersi); `totp_secret_sealed ≥ 44 bayt`
>     (12 nonce + 128 bit sır tabanı + 16 etiket; kesin düzen OP-6); `totp_last_step NOT NULL
>     DEFAULT 0`.
> 14. **OP-10'a devredilenler:** (a) bilet ömrü **60 sn'den kısa** seçilmeli: `created_at`
>     DEFAULT'u ile `clock_timestamp()` + ömür iki ayrı saat okumasıdır, tam 60 sn tavanı
>     mikrosaniyeyle kaçırabilir; (b) `op_begin_read`'in audit `INSERT … RETURNING id`'si
>     `tappa_opdefiner`'a `operator_audit_log` üzerinde `SELECT (id)` ister — OP-5 vermedi
>     (kullanan yok); (c) bilet `kind` CHECK'i bugün yalnız şekil, türler OP-10'da.
> 15. **OP-7'ye devredilenler:** `tappa_operator`'ın **girişi** (dev parolası
>     `02-dev-only-password.sh` emsaliyle, üretim parolası bir pod'a `secretKeyRef` ile —
>     ⚠️ `10-postgres.yaml`'a `tappa-secrets`'ta henüz olmayan bir anahtar için
>     `optional` olmayan bir `secretKeyRef` eklemek Postgres pod'unu başlatamaz); ADR 0021
>     §2 vi'nin `db/queries/operator.sql` belgesi ve `internal/db/operator.go` erişimcileri
>     (OP-5'te Go erişimcisi yok, bu yüzden yazılmadı).
> 16. *(2. turda kapandı — madde 21.)* **Açık iş (bu görev `scripts/`'e dokunmadı):** `scripts/pg-restore-verify.sh` 5. bölüm
>     TRUNCATE korumasını **altı** append-only tabloda doğruluyor; `operator_audit_log`
>     yedincidir ve orada yok. `scripts/redline-check.sh`'ın `APPEND_ONLY` deseni onu
>     **tesadüfen** yakalıyor (`[^ ]*audit_log` alt dizesi); dosyanın kendi cümlesi *"bir
>     yedincisi eklenirse iki yer birden güncellenmelidir"* diyor.
> 17. **Kalıcı test verisi:** eşzamanlılık testi (`TestOpCompleteEnrollment_ConcurrentRaceExactlyOneWinner`)
>     commit etmek zorunda; her koşu bir operatör hesabı, bir oturum ve bir `enrollment`
>     audit satırı bırakır (rastgele uuid'li, `@example.test`). *(3. turda düzeltildi —
>     "diğerlerinin hepsi geri alınır" cümlesi 2. turdan beri eskiydi:)* ikinci bir istisna
>     `TestOpRecordAuthEvent_NoLockOracleOnAnInvisibleAccount`'tır — iki oturum gerektiği
>     için üç hesabı commit eder, iki sonda transaction'ını geri alır (audit satırı commit
>     olmaz) ve hesapları sonunda siler. Ölçüldü (hesap/oturum/audit): eşzamanlılık testi
>     7/7/7 → 8/8/8, kilit-kehaneti testi 8/8/8 → 8/8/8. Geri kalanların hepsi tek
>     `REPEATABLE READ` transaction'ında geri alınır.
>
> **Kabul — OP-5 listesi:** her maddenin testi yukarıdaki iki test dosyasında; mutasyonla
> kırmızıya dönenler görevin raporunda tablo hâlinde. ~~(yeşil kalan tek mutasyon adıyla:
> yalnız `enroll_used_at IS NULL`'ı silmek eşzamanlı enrollment'ı **kırmaz**, çünkü başarı
> durumu `active` yaptığı için `status = 'pending'` yüklemi tek kullanımı tek başına taşır;
> ikisi birlikte silinince 24 yarışçının 24'ü kazanır).~~ **2. turda YANLIŞLANDI (B1):**
> `status = 'pending'` tek kullanımı yalnız hesap bir daha `pending`'e dönmedikçe taşır.
> Sahip kullanılmış bir hesabı token'ı değiştirmeden `pending`'e aldığında (tam da özensiz
> bir `reset-mfa`'nın ürettiği durum) o mutant hesabı **eski** token'la yeniden enroll etti
> ve 1. turun bütün testleri yeşil kaldı (denetçi ölçtü). Düzeltme aşağıda, madde 18.

> **Kart düzeltmesi (2026-09-26, OP-5 uygulaması sırasında — 2. tur: üçüncü göz RED, 1
> bloklayan + 4 orta + 8 düşük).** Hepsi kapatıldı ya da ölçümle sayılı sınıra yazıldı;
> migration `00026` değişti (dev: down 26→25, up 25→26).
>
> 18. 🔴 **B1 · tek kullanım iki BAĞIMSIZ katman.** (a) Şema:
>     `platform_admins_pending_token_unused CHECK (status <> 'pending' OR enroll_used_at IS
>     NULL)` — `TestOperator00026_TableShapeChecks` onu kısıt **adıyla** pinler (kullanılmış
>     bir hesabı eski token'la `pending`'e almak 23514; `reset-mfa` şekli — yeni hash,
>     `enroll_used_at = NULL` aynı ifadede — kabul). (b) Fonksiyon:
>     `TestOpCompleteEnrollment_AUsedTokenIsRefusedByTheFunctionItself` CHECK'i
>     transaction içinde düşürür, `pending` + kullanılmış + canlı durumu kurar, eski token →
>     28000; kontrol: `enroll_used_at` temizlenince aynı çağrı kabul. İki mutant (fonksiyon
>     yüklemi silinmiş / CHECK silinmiş) **ayrı ayrı** kırmızı. **OP-9'a, adıyla:**
>     `reset-mfa` token hash'ini değiştirir ve `enroll_used_at`'i **aynı ifadede** `NULL`
>     yapar (CHECK aksini zaten reddeder).
> 19. **O1 · kısıt DETAIL sızıntısı ve kehanet.** *(3. turda iki cümlesi düzeltildi —
>     aşağıdaki "D-1" ve "D-2" notları.)* Ölçüldü (1. tur şeması): geçerli token +
>     cost-11 digest → 23514 ve DETAIL *"Failing row contains (…)"* — yazılan digest, zarfın
>     hex'i, adres, enrollment hash'i; tekrar eden oturum hash'i → 23505
>     `Key (token_hash)=(…)`; ikisi de yalnız bütün koşullar geçince çıkıyordu. Karar:
>     argümanı bir kısıta ulaşan iki fonksiyon (`op_open_session`,
>     `op_complete_enrollment`) `integrity_constraint_violation`'ı yakalayıp **aynı**
>     28000'e çevirir (her şey geri alınır, 0 audit); geride yalnız kısıt adı + SQLSTATE
>     taşıyan bir LOG satırı kalır. **D-1 (3. tur):** o satır *"sunucuya yalnız"* DEĞİLDİR
>     ve *"koşullar tuttu"* kehaneti kapanmadı — `client_min_messages` kullanıcı ayarıdır,
>     `SET LOCAL client_min_messages = log` diyen çağırana satır döner ve yalnız bütün
>     koşullar tuttuğunda doğar (ölçüldü: koşullar tutarken bozuk hash → çağırana `LOG: …
>     failed constraint … (SQLSTATE 23514)`; bayat adımla aynı hash → LOG yok). Kapanan
>     değer sızıntısı ve DETAIL'dir; bit, SAVEPOINT kanalının verdiğiyle aynı ve ADR 0021
>     sayılı sınır 14'e eklendi. Ölçülüp alınmayan kapatma: fonksiyona `SET
>     client_min_messages = error` (satırı çağırandan keser, sunucu yine yazar — ölçüldü)
>     — iki eşdeğer kanaldan birini kapatır, öğrenilebileni azaltmaz, §6'nın tek-girdili
>     `proconfig` pinini değiştirirdi. Şekil ön kontrolü **seçilmedi**: her CHECK regex'inin ikinci bir
>     kopyası olur (kaydığı gün sızıntı geri gelir) ve tekrar eden hash'i yarışsız
>     kapsayamaz. Kalan üç fonksiyonun argümanı hiçbir kısıta ulaşmaz (touch/close yalnız
>     `WHERE`'de; record türü yazmadan önce doğrular). Ölçüm (dev, 1 koşu): 14 yakalanan ret,
>     sunucu log'unda **0** *"Failing row contains"* / **0** `Key (token_hash)`, **14** LOG
>     satırı (yalnız kısıt adı + SQLSTATE). Pin: `TestOperator00026_ArgumentsNeverComeBackInAnError`
>     (her şekilce bozuk girdi 28000, DETAIL/HINT boş, hiçbir alanda argüman değeri yok, 0
>     audit; sonunda aynı çağrı geçerli argümanla kabul). **OP-7'ye, adıyla:** Go tarafı
>     `PgError.Detail`'i asla log'lamaz. ⚠️ Ayrı gözlem: dev `docker-compose` Postgres'i
>     `log_statement=all` ile koşuyor ve **bind parametrelerini** (ham token dahil) sunucu
>     log'una yazıyor — ADR 0021'in *"commit'ten bağımsız ikinci iz / log_parameter_max_length"*
>     açık maddesinin dev yüzü. **D-2 (3. tur): üretim ÖLÇÜLDÜ — orkestratör, salt-okunur,
>     2026-09-26:** `log_statement = none` · `log_min_duration_statement = -1` ·
>     `log_parameter_max_length = -1` · `log_parameter_max_length_on_error = 0` ·
>     `log_min_error_statement = error` · `log_min_messages = warning` ·
>     `log_error_verbosity = default`. İki koşul adlandırıldı (ADR 0021 "Karar verilmedi"):
>     `log_parameter_max_length_on_error` **0 kalmalı** (her `op_*` reddi ERROR,
>     `log_min_error_statement = error` STATEMENT'ı log'lar; ~~> 0 olursa~~ **0 değilse —
>     `-1` dahil —** ham enrollment token'ı, oturum hash'i ve digest pod log'una **ve
>     çağıranın hata CONTEXT'ine** gider; **4. tur düzeltmesi, madde 33:** bu ayar
>     `user` bağlamlıdır, yani işletme onu tek başına garanti EDEMEZ) · `log_min_duration_statement`
>     açılırsa `log_parameter_max_length = -1` yavaş bir `op_*` çağrısının parametrelerini
>     tam log'lar. **OP-7'ye, adıyla:** operatör sorguları yalnız bağlı parametreyle; SQL
>     metnine değer gömülmez.
> 20. **O2 · `op_close_session`'ın oturum yüklemi pinlendi.** `op_touch_session`'ın ölü-oturum
>     tablosu (mutlak, boşta, MFA'sız, iptal, `disabled`, bilinmeyen) ortak bir yardımcıda;
>     `TestOpCloseSession_RefusesEveryDeadSession` aynısını close için koşar (28000,
>     `revoked_at` değişmez, 0 audit). Oturumu touch yerine doğrudan arayan mutant kırmızı.
> 21. **O3 · yedinci append-only tablo iki script'te.** `scripts/pg-restore-verify.sh` 5.
>     bölüm `trunc_tables`'a `operator_audit_log` eklendi, sayılar 6 → 7;
>     `scripts/redline-check.sh` `APPEND_ONLY`'ye açıkça yazıldı (1. turda `[^ ]*audit_log`
>     alt dizgisiyle tesadüfen yakalanıyordu). Yeni türetilmiş test
>     `TestAppendOnlyTablesAreNamedByBothScripts` (`cmd/tappa`): append-only kümeyi
>     migration'lardaki `tappa_forbid_mutation` satır tetikleyicilerinden türetir ve iki
>     listeyle **iki yönde** eşitlik + her birinde TRUNCATE koruması ister. Madde 16'nın
>     açık işi kapandı. ~~sekizinci bir tablo artık hatırlanmadan unutulamaz~~ — **D-6 (3.
>     tur):** o cümle fazlaydı; türetme yalnız `BEFORE UPDATE OR DELETE` yazımını tanıyordu
>     ve `DELETE OR UPDATE` yazımlı sekizinci bir tablo (depo dışında tutulan geçici bir
>     migration'la ölçüldü) testi **yeşil** bıraktı. Türetme artık her `CREATE [OR REPLACE]
>     [CONSTRAINT] TRIGGER` ifadesini parçalarıyla sınıflar (hedef, `ROW`/`STATEMENT`,
>     TRUNCATE; olay sırası, harf, satır sonu, `EACH`/`PROCEDURE`/`public.`/tırnak
>     serbest) — aynı sonda artık **kırmızı**; yazımları `TestAppendOnlyTriggers_EverySpellingIsSeen`
>     pinler. **Ölçülmüş sınırı:** dinamik SQL'le (`EXECUTE format(...)`) yaratılan bir
>     tetikleyiciyi, `tappa_forbid_mutation` DIŞINDA bir fonksiyona bağlı bir değiştirme
>     yasağını ve yalnız yetkiyle append-only yapılmış bir tabloyu görmez. *(4. tur, madde
>     36: bir `DO` bloğunun ya da fonksiyon gövdesinin içindeki **statik** `CREATE TRIGGER`'ı
>     da görmüyordu — artık görüyor.)* **Ters yöndeki sınır (5. tur, denetçi kopyada
>     ölçtü):** hiç çalışmayan metni de sayar — `IF false` altındaki, hiç çağrılmayan bir
>     fonksiyonun gövdesindeki ya da bir string literalinin içindeki `CREATE TRIGGER`.
>     Satır tarafında fail-closed (listelenmesi gerekmeyen bir tabloyu ister), TRUNCATE
>     tarafında **fail-open** (koruması olmayan bir tablonun korumasını karşılar); o yönün
>     arka kapısı `scripts/pg-restore-verify.sh` 5. bölümün `pg_trigger` katalog
>     kontrolüdür. Bugünkü ağaçta böyle metin yok.
> 22. **O4 · runbook kuralı.** Her `kubectl` satırı `--context hetzner-k8s-1 -n tappa`; psql
>     kullanıcıyı ve veritabanını pod'un kendi `POSTGRES_USER`/`POSTGRES_DB`'sinden okur (tek
>     tırnaklı `sh -c`). 0. adım hash basmak yerine **kesimi doğrular**: `psql -1` boş girdiyle
>     exit 0 verir (ölçüldü) — yanlış yazılmış bir işaret 1. adımı sessizce boş geçirirdi;
>     artık blokta 2 `CREATE ROLE` + kapanış işareti = **3** sayılır (dev'de ölçüldü: 3).
>     ⚠️ `deploy/README.md`'nin **diğer bölümlerindeki** kubectl satırlarının hiçbiri
>     `--context` taşımıyor (ölçüldü: 138 kubectl satırının 0'ı); onlar OP-5'in kapsamı
>     dışında, dokunulmadı — orkestratörün kararı.
> 23. **D1 · ön koşul `tappa_opdefiner`'ın KENDİ üyeliklerini de sınar.** Ölçüldü: transaction
>     içinde `GRANT tappa_owner TO tappa_opdefiner` → eski ön koşul geçti, definer
>     `tags.aes_key_ref` ve `admin_users.password_hash` üzerinde SELECT=t. Ön koşul + ters
>     katalog pini + ön koşul testi kontrolü eklendi; kontrolü silen mutant kırmızı.
> 24. **D2 · kilit-çekişmesi kehaneti kapandı.** Sayaç `UPDATE`'i `status = 'active'` ile
>     süzülür. Ölçüldü (1. tur şeması, iki oturum): açık tutulan bir `totp_failed` çağrısı
>     `pending` ve `disabled` hesapta ikinci çağıranı `55P03`'e düşürdü. Şimdi:
>     `TestOpRecordAuthEvent_NoLockOracleOnAnInvisibleAccount` — `pending`, `disabled` ve
>     bilinmeyen id anında döner; kontrol: `active` hesapta `55P03` (sonda çekişmeyi
>     görebiliyor; aktif hesapları `tappa_operator` zaten SELECT eder). Aktif olmayan
>     hesapta `totp_failed` sayacı ilerletmez (pinli). *(4. tur: kapanan yalnız **kilit**
>     kanalıdır; aynı gizli hesapların adres varlığı istatistik görünümlerinden hâlâ
>     okunur — madde 34, sayılı sınır 15.)*
> 25. **D3 · `unknown_email` + hedef belgelendi ve pinlendi.** Tür Go'nun görüşü (RLS
>     yalnız `active`'i gösterir), hedef veritabanının araması: `pending`/`disabled` bir
>     adres `unknown_email` satırı ve o hesabın id'si. ADR 0021 §1'e not.
> 26. **D4 · kilit şekli bilinçli ve pinli:** sayaç yalnız başarıda sıfırlanır; pencere
>     geçince tek hata 15 dk yeniden kilitler, kilitliyken hata pencereyi uzatır. İlk
>     kilitten sonra pencere başına en çok 1 tahmin (günde 96); sayacı pencere sonunda
>     sıfırlamak N tahmin verirdi (günde 480). ADR 0021 uygulama notu.
> 27. **D5 · OP-9'a, adıyla:** `enroll_issued_at` ve `enroll_expires_at`'in **ikisi de** SQL
>     içinde tek bir `clock_timestamp()` okumasından yazılır — süre tavanı yazılabilir
>     `enroll_issued_at`'e bağlıdır (migration'ın kendi uyarısı).
> 28. **D6 · gölge testi beş fonksiyonun hepsini koşar:** `op_open_session` (gerçek hesabın
>     adım geçmişi olmayan sahte kopyası — gölgeden okunsaydı tekrar eden kod oturum açardı;
>     gerçek `totp_last_step` ilerlemeli) ve `op_complete_enrollment` (gerçek pending hesabın,
>     çağıranın seçtiği token'ı taşıyan sahte kopyası). Hesap tablosunu nitelemeyen iki
>     mutant kırmızı.
> 29. **D7 · ADR 0021 §2 v 7 kapsam notu düzeltildi:** `SAVEPOINT` içinde `op_open_session`
>     iz bırakmadan `tappa_operator`'ın SELECT edemediği `totp_last_step`'in yerini ve
>     ~~sayacın eşiğe ulaşıp ulaşmadığını~~ ele verir. Sayılı sınır 14 (sınır 1'in içinde).
>     **4. tur düzeltmesi (madde 35):** kilit yarısı yeni bilgi değildir —
>     `totp_locked_until` `tappa_operator`'ın giriş sütunlarındadır ve doğrudan okunur;
>     SELECT edilemeyen yalnız adım yarısı.
> 30. **D8 ·** `db/queries/operator.sql` OP-7'de (değişmedi).

> **Kart düzeltmesi (2026-09-26, OP-5 uygulaması sırasında — 3. tur: üçüncü göz ONAY, 6
> düşük bulgu).** D-1, D-2, D-5 ve D-6 yukarıda, düzelttikleri maddelerin içinde (17, 19,
> 21). Kalan ikisi:
>
> 31. **D-3 · ön koşul `tappa_operator`'ın ÜYELERİNİ de sınar.** Ölçüldü (1.–2. tur şeması,
>     `BEGIN … ROLLBACK`): `GRANT tappa_operator TO tappa_app` →
>     `has_function_privilege(tappa_app, op_record_auth_event, EXECUTE)` false → true ve
>     `tappa_app` fonksiyonu çağırdı. Ön koşul artık reddeder (55000); ön koşul testine ve
>     ters katalog pinine kontrol eklendi; kontrolü silen mutant kırmızı. Dört üyelik yönü
>     (definer'ın üyesi, definer'ın üyeliği, operatörün üyesi, operatörün üyeliği) artık
>     dördü de reddediliyor.
> 32. **D-4 · `tappa_opdefiner`'ın tenant erişimi pinli.** `TestOperator00026_PrivilegeMatrix`
>     artık (a) ADR 0021 §1'in yedi "asla" sütununda definer'ın SELECT'inin `false`
>     olduğunu, (b) definer'ın dört operatör tablosu dışındaki **her** tablo ve görünümde
>     SELECT/INSERT/UPDATE sütun listesinin ve DELETE/TRUNCATE/REFERENCES/TRIGGER'ın izin
>     listesine (`opdefinerTenantGrants`, OP-5'te **boş**) eşit olduğunu, (c) definer'ın ve
>     operatörün hiçbir dizide yetkisi olmadığını sınar. Denetçinin mutantı (`tags`
>     `aes_key_ref`/`app_key_ref` ve `admin_users.password_hash` SELECT'i) kırmızı.
>     **Nasıl genişler (OP-10+):** bilinçli her grant, migration'ıyla aynı değişiklikte
>     `opdefinerTenantGrants`'a `"tablo:YETKİ"` → attnum sıralı **tam** sütun listesi (tablo
>     düzeyi fiil için `"*"`) olarak eklenir; "asla" sütunları o listeye giremez (test
>     listeyi de onlara karşı denetler).

> **Kart düzeltmesi (2026-09-26, OP-5 uygulaması sırasında — 4. tur: tappa-security-auditor
> ONAY, 1 orta + 3 düşük).** Düzeltilen eski maddeler yerinde işaretli (19, 21, 24, 29).
> Migration'da yalnız bir yorum değişti (fonksiyon gövdesi yok).
>
> 33. **S-1 · `log_parameter_max_length_on_error` işletmenin tek başına tutabileceği bir
>     koşul DEĞİL.** Ölçüldü (dev, `BEGIN … ROLLBACK`): ayarın `pg_settings.context`'i
>     `user` (`log_parameter_max_length`'inki `superuser`); `tappa_operator` olarak
>     `SET log_parameter_max_length_on_error = -1` → `-1`; `tappa_operator` olarak
>     `ALTER ROLE tappa_operator SET log_parameter_max_length_on_error = -1` **başarılı**,
>     `pg_db_role_setting`'de 1 satır (parola döndürmesinden sağ çıkar, havuzun her yeni
>     bağlantısına uygulanır); `-1` iken reddedilen `op_touch_session($1)` (bağlı parametre,
>     `\bind`) çağıranın CONTEXT'ine `unnamed portal with parameters: $1 = '…'` döndürdü
>     (`0` iken yalnız fonksiyon satırı; sunucu log'u yarısını güvenlik denetçisi ölçtü).
>     Metin ADR 0021'de ve madde 19'da düzeltildi (`-1` de sızdırır). **OP-7'ye, adıyla:**
>     yukarıdaki "Kabullere bağlananlar — OP-7" maddesinin (a) şıkkı. **Bugünkü ucuz pin:**
>     `TestOperator00026_ReverseCatalogPin` `pg_db_role_setting`'de iki operatör rolü için
>     satır olmamasını ister (iki kontrol; kaldıran mutant kırmızı); runbook'un 2. adımı
>     sayıyı gösterir (dev'de `…|0`, belgeyle birebir). ⚠️ **Kapsam dışı, not (orkestratör
>     backlog'a alır):** aynı düğme `tappa_app` için de açıktır — `user` bağlamlı ayarı
>     müşteri uygulamasının rolü de kendi oturumunda ve rol varsayılanı olarak çevirebilir;
>     panelin bağlı parametreleri (oturum token hash'leri, davet kodu hash'leri) aynı
>     yoldan log'a düşer.
> 34. **S-2 · istatistik görünümleri bir varlık kehaneti — sayılı sınır 15.** Ölçüldü (dev,
>     `tappa_operator`, her çağrı ayrı `SAVEPOINT` + `ROLLBACK TO`): `platform_admins`
>     `pg_stat_xact_user_tables.seq_scan` farkı — bilinmeyen adres **+1** (iki ayrı adreste
>     aynı), `pending` hesabın adresi **+2**, `disabled` hesabın adresi **+2**
>     (`seq_tup_read` de farklı); fazlalık `target_admin_id` FK kontrolü.
>     `pg_stat_user_tables.n_live_tup` gizliler dahil toplam hesap sayısını verir.
>     Görünümler herkese açık → definer tarafında kapatılamaz; sayaçlar plana bağlı olduğu
>     için "eşitleyen" bir yama bir sonraki planda bozulur. Ölçülen ama **uygulanmayan**
>     daraltma: adres yolunu yalnız `active` hesaplara çözmek (2. turun D3 kararını geri alır,
>     plan değişince aynı sınıf `idx_tup_fetch`'e taşınır). Düzeltilen cümleler: migration
>     `op_record_auth_event` yorumu (*"no existence oracle"*), ADR 0021 §1 (*"üyelik
>     kehaneti değildir"*), ADR 0021 uygulama notu D2 (yalnız kilit kanalı kapandı).
> 35. **S-3 · sınır 14 ve §2 v 7 kilit yarısını büyük gösteriyordu.** Ölçüldü:
>     `tappa_operator` olarak 5 `totp_failed` sonrası `SELECT totp_locked_until` kilidi
>     okudu; `SELECT totp_last_step` → *permission denied*. Kehanetin yalnız **adım** yarısı
>     yeni bilgidir; iki metin ve madde 29 düzeltildi.
> 36. **S-4 · sınıflandırıcı DO bloğundaki statik `CREATE TRIGGER`'ı da görür.** Ucuz olduğu
>     için genişletildi (sınıra yazmak yerine): `create … trigger` artık `;`-parçasının
>     başında değil, parçanın içinde nerede başlarsa oradan okunur;
>     `TestAppendOnlyTriggers_EverySpellingIsSeen`'e iki yazım eklendi (DO içinde satır
>     tetikleyicisi, DO içinde TRUNCATE koruması); eski, başa bağlı desene dönen mutant
>     kırmızı. Kalan sınır (madde 21): **dinamik** SQL (tablo adı metinde yok) ve ters
>     yönde **çalışmayan statik metin** (5. tur; TRUNCATE tarafında fail-open, arka kapısı
>     `pg-restore-verify.sh` 5. bölüm).

### Görevler — A2 tenant-ötesi okuma/yazma
| ID | Görev | Efor | Kabul (özet) |
|---|---|---|---|
| OP-11 | Tenant listesi/arama/detay (`op_list_tenants`, `op_tenant_detail`) | M | tappa_app EXECUTE yok; tappa_operator `tenants` doğrudan SELECT yok; süresi geçmiş/MFA'sız hash → exception; her çağrı tam 1 operatör audit satırı; anahtar/hash sütunları için `has_column_privilege(opdefiner,…)=false`; tenant başlıkta adıyla |
| OP-12 | Faturalama görünümü (salt-okuma) | M | tutarlar tenant önizlemesiyle aynı (fixture'da çapraz test); `numeric`, float değil |
| OP-13 | Plaket envanteri (salt-okuma) | S–M | anahtar sütunlarına erişim yok (katalog testi) |
| OP-14 | Operatör audit görüntüleyici | S | — |
| OP-15 | Askıya alma / yeniden etkinleştirme (ADR 0025) | L | router'dan türetilen HER yazma rotası 403 + audit; GET/CSV 200; askıdaki tenant'ın NFC/QR tap'i askısız ile AYNI karar + trust (tablo testi, §4.6); tappa_app `suspended_at` UPDATE edemiyor; iki audit satırı aynı tx (rollback testi); reinstate yazmaları açıyor |
| OP-16 | VAT yeniden doğrulama | M | tappa_app `vat_*` hâlâ UPDATE edemiyor; tenant audit önce/sonra; VIES kesintisinde yazma yok, cümle gösteriliyor |
| OP-17 | Plan/fiyat | M–L | **T27/T37 kararına bloke** |
| OP-18 | Operatör encode'u + tenant tarafı encode'u kapatma (ADR 0017 md.12 kapanır; A-2, A-7) | L | Android uygulaması operatör host'una giriş yapar; önce operatörün hedef tenant'a yazabilmesi |
| OP-19 | Süreç ayrımı (aynı imaj, ayrı Deployment) | — | K9: pilot sonrası yeniden bak |

## 4. Akış B — E-posta (AWS SES)

### Öneri: SES SMTP arayüzü + stdlib `net/smtp` (STARTTLS 587), `eu-central-1` — ✅ (sıfır yeni modül)
| | (a) SES SMTP + `net/smtp` | (b) SES v2 HTTP + elle SigV4 | (c) `aws-sdk-go-v2` |
|---|---|---|---|
| Yeni modül | **0** | **0** | servis başına ≥4; `config` ile +8-9 |
| Kripto | yok (TLS stdlib) | HMAC zinciri bizde — §3 "kripto yalnız `internal/sun`" gerilimi | SDK'da |
| Test/dev | aynı kod yolu sahte SMTP'de ve dev yakalayıcıda koşar | yalnız SES | yalnız SES |
| Bakım | `net/smtp` dondurulmuş ama Go güvenlik politikası kapsamında | imza + saat kayması (±5 dk) bizde | govulncheck yüzeyi, sık sürüm |

### Tasarım özü (ADR 0022'de normatif)
- **`internal/mail`** (yalnız stdlib; davet/reset kavramını bilmez): `Message{To, Subject, Text,
  HTML, Ref}`, `Receipt{MessageID}`, `SMTP.Send(ctx, Message)`. `DialContext` + deadline →
  STARTTLS **zorunlu** (ilan edilmezse hata, düz metne düşüş yok; TLS ≥1.2) → PlainAuth →
  Mail/Rcpt → DATA elle (250 yanıtından Message-ID) → Quit. 4xx için bellekte tek geri çekilmeli
  deneme; link kalıcı bir kuyruğa YAZILMAZ.
- 🔴 **Sızıntı kuralı:** `SendError{Class, SMTPCode}` — `Error()` sunucu metni ya da adres
  İÇERMEZ, `textproto.Error`'ı SARMAZ (Unwrap yok). Ölçülen gerekçe: davet hata yolu bugün hata
  zincirinin tamamını logluyor (`employeeactions.go:419`); SES sandbox reddi alıcı adresini metinde
  taşır → log'a düşerdi. Sınıflar: invalid_address · tls_unavailable · auth · rejected · throttled ·
  network · timeout.
- Başlık üreticisi CR/LF/kontrol karakteri içeren değeri REDDEDER (temizlemez); alıcı
  `mail.ParseAddress` birebir (Name boş, Address = girdi), tek, ASCII, ≤254. SMTP parolası redakte
  eden tipte (`invite.Code` deseni: Format/String/GoString/LogValue).
- **Config (akış başına):** `TAPPA_RESET_DELIVERY` none|email · `TAPPA_INVITE_DELIVERY`
  panel|email · `TAPPA_SMTP_HOST` (prod'da localhost/IP literal yasak) · `TAPPA_SMTP_PORT` (587;
  465 red — STARTTLS tasarımı, Hetzner engeli) · `TAPPA_SMTP_USERNAME` / `TAPPA_SMTP_PASSWORD`
  (**Secret**, `optional: true`) · `TAPPA_MAIL_FROM` (`Taptime <no-reply@taptime.mt>`) ·
  `TAPPA_MAIL_REPLY_TO` (ops.). Bir akış `email` iken SMTP ayarı eksikse boot hatası (fail-closed).
  `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` yeni env'leri manifestte zorlar.
  R7 tuzağı: config hata mesajında değişken adı `%s` ile basılır.
- 🔴 **Reset gönderimi istek yolundan ÇIKAR (zorunlu):** senkron SES (TLS + AUTH + DATA) 250 ms
  tabanını aşar → kayıtlı/kayıtsız adres zamanlamayla ayırt edilir (`adminresetlimits.go:308-316`
  bunu öngörüyor). Token'lar senkron basılır; grant'lar sınırlı süreç-içi işçiye (tampon ~32, 1-2
  goroutine, `context.WithoutCancel` + 15 s), işçi mevcut `h.deliver`'ı çağırır (gönder-sonra-tek-
  audit korunur); kuyruk doluysa senkron `undelivered`; kapanışta ≤3 s boşaltma
  (`shutdownbudget_test.go`). Artık risk (gönderim anında çökme → audit'siz reset) ADR'ye.
- **Davet:** email modunda `IssueAndDeliver`'dan ÖNCE adres okunur (yeni sqlc sorgusu, tenant + id,
  işletme adıyla); yok / geçersiz / **aynı tenant'taki bir yöneticinin adresine eşit** (citext) →
  kod basılmaz, net cümle + audit. Ekranda "`<adres>` adresine gönderildi, 7 gün geçerli".
  "E-postayı değiştir" aksiyonu bekleyen davetleri aynı tx'te iptal eder
  (`CancelPendingInvitesForEmployee`). **Y-D (ADR 0005) kapanmaz, DARALIR** — müdür adresi kendisi
  yazıyor; plus-adresleme/alias artık risk; ADR 0005'e append.
- **Şablonlar:** yalnız İngilizce; UTF-8 + RFC 2047 konu (Maltaca ċ ġ ħ ż); HTML satır içi stil,
  tappa-brand paleti, metin wordmark "Taptime" — görsel / uzak font / dış URL / izleme pikseli YOK,
  tek mutlak URL eylem linki (testle); düz metin kısmı (link kopyalanabilir); sabit konu;
  tenant/çalışan adı yalnız gövdede, escape'li. Davet metni: "telefonun ana tarayıcısında açın"
  (iOS Gmail/Outlook uygulama-içi tarayıcısı çerezi Safari ile paylaşmaz; NFC varsayılan tarayıcıyı
  açar — state.md'de ölçülmüş tuzak).
- **Oran sınırları (herkese açık signup = spam vektörü):** tenant 50/saat + 300/gün, çalışan
  3/saat, süreç geneli ~300/saat devre kesici (sayılar uygulamada aritmetikle savunulur).
- **Log:** yalnız tenant_id, employee_id/admin_user_id, message_id, class, smtp_code — adres,
  link, sunucu metni ASLA.
- **SES:** yapılandırma seti TLS=Require, açılma/tıklama izleme KAPALI (tıklama izleme linki AWS
  yönlendiricisinden geçirir = sırrın üçüncü tarafa ifşası), hesap düzeyi bastırma listesi açık.
  IAM yalnız `ses:SendRawEmail`, kaynak identity + config set ARN, koşul `ses:FromAddress`.
- **"Shown once":** `panel` modu kalır; adresi olmayan çalışan için "linki göster" yedeği yalnız
  owner'a, `manager_panel` kanalı olarak audit'lenir (M6-11'de iki kanal ayrı görünür).

### Görevler
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| EM-1 | ADR 0022 + Q02 "Cevaplananlar"a + ADR 0005 Y-D append ("daraldı, kapanmadı") + ADR 0015 durum notu + M7-04/M7-07 notları | S | orkestratör | ADR'de (b)/(c) ölçüleriyle elenmiş; Y-D "kalkar" değil "daralır" | — |
| EM-2 | `internal/mail` SMTP taşıyıcısı | M–L | builder + güvenlik | test içi sahte SMTP (öz-imzalı): STARTTLS ilan edilmezse ClassTLS + sunucu 0 AUTH görür; 250'den Message-ID; takılan sunucuda deadline ≤100 ms; To/Subject'e `\r\n` → 0 dial ile hata; 30 s fuzz panik/CRLF sızıntısı yok; sunucu `550 … <adres> … ?t=TOKEN` dönerse `Error()`/`%+v`/slog JSON'da adres de token da YOK; parola her biçimde redakte | EM-1 |
| EM-3 | Config + manifest (`05-config.yaml`, `20-app.yaml` iki opsiyonel `secretKeyRef`) + runbook | S–M | builder | fail-closed matris tablo testi (her eksik anahtar ayrı vaka); packaging testi yeşil; prod'da localhost/465 red; ConfigMap hâlâ none/panel (davranış değişmez) | EM-1 |
| EM-4 | Şablonlar (`web/templates/email/*.templ` + metin üreticileri) | M | builder + tappa-brand | tam 1 mutlak URL; `<img`/`@font-face`/`<link`/`<script` 0; `<script>`'li ad escape; Maltaca doğru; konu `=?utf-8?q?`; kontrast AA (hesaplanmış) | EM-1 |
| EM-5 | Asenkron reset teslimi + `emailResetChannel` + main `case email` | M–L | builder + güvenlik | kanal gecikmesi 2 s iken kayıtlı/kayıtsız medyanları [taban, taban+50 ms] (yeni TimingIsFlat; senkron kodda kırmızı olduğu gösterilir); grant başına tam 1 audit; kuyruk dolu → senkron undelivered; kapanış bütçesi yeşil; M7-04'ün sahteye karşı koşmuş kriterleri GERÇEK SMTP'ye karşı yeniden koşulmuş (tablo rapora); canlı duman: Gmail "Show original" SPF=PASS (mail.taptime.mt), DKIM=PASS, DMARC=PASS | EM-2,3,4 + kullanıcı dış adımları |
| EM-6 | Adres okuma + "e-postayı değiştir" aksiyonu | M | tappa-db-migrator + builder + tappa-brand | RLS izolasyon (A, B'nin adresini okuyamaz); değişiklik bekleyen davetleri aynı tx'te iptal; audit; log'da `.Email` yok (R7b); migration beklenmiyor (ölç) | EM-5 |
| EM-7 | `invite.EmailChannel` + `emailLinkSink` + davet anahtarı + limitler | M–L | builder + güvenlik + tappa-brand | email modunda gövdede `/activate?code=` 0; `invite.code_emailed` 1, `code_shown_to_manager` 0; adres yok/geçersiz/yönetici adresi → `employee_invites` satırı 0; N+1. davet oran sınırında red, satır basılmaz; SMTP hatası log'da yalnız class/code (`employeeactions.go:419` yolu sızıntı testiyle kapalı); yedek yalnız owner'a + `manager_panel` audit | EM-6 |
| EM-8 | M6-11 kanal ayrımı + gerçek cihaz turu | S + kullanıcı | builder + kullanıcı | Gmail Android/iOS, Outlook, Apple Mail'den aktivasyon → NFC tap'te oturum tanınıyor; sonuç tablosu state.md'de | EM-7 |
| EM-9 | "Parolanız değişti" bildirimi (linksiz, yalnız giriş sayfası adresi) | S | builder | — | EM-5 |
| EM-10 | Bounce/complaint (SNS → HTTPS, imza doğrulama) | L | — | pilot sonrası, kendi ADR'si | — |
| EM-11 | Signup e-posta doğrulaması (ADR 0013 c) | L | — | pilot sonrası, kendi ADR'si | — |
| EM-12 | M7-07 yönetici daveti (kilidi açılır) | — | — | kendi kartı ve ADR'si | EM-7 |

> **Kart düzeltmesi (2026-09-25, F0-5 uygulaması sırasında — EM-3 için): SMTP kimlik bilgisi
> biçimi R7d'de şu durumda.** SES SMTP kullanıcı adı bir AWS erişim anahtarı kimliğidir
> (`AKIA…`, 20 karakter) ve `aws-key` sınıfı onu nerede geçerse geçsin FAIL verir. SMTP
> parolası 44 karakterlik base64'tür: `+` ya da `/` taşıyorsa zaten yakalanıyordu; taşımıyorsa
> (yalnız harf+rakam) 3. tura kadar tanımlayıcı sayılıp SESSİZ kalıyordu. Artık ≥32 karakterlik,
> büyük+küçük harf+rakam karışık, tekrarlı dolgu olmayan bir değer bir sır kelimesiyle aynı
> satırda tırnak/backtick içindeyse `a0-token` FAIL verir (ağaçta 0 yanlış pozitif; iki
> sentetik test değeri tabloda adıyla). KALAN: tırnaksız yazılmış ya da sır kelimesinden ayrı
> satırdaki değer, `_`/`-` taşıyan base64url biçimi (Go test adlarıyla aynı şekil) ve
> `Authorization: Basic …` başlığı yakalanmaz (`scripts/secretscan.sh` sınır listesi). EM-3'ün
> runbook'u SMTP değerlerini yalnız Secret'a yazar; hiçbir belgeye değer yazılmaz (A-0).

### Kullanıcının dış adımları (EM-2 ile paralel başlar; sıralı)
1. AWS hesabı: root için MFA, günlük kullanım için ayrı yönetici kullanıcı, fatura alarmı (~$5).
2. SES `eu-central-1` → Identities → Domain `taptime.mt`: Easy DKIM (RSA 2048); Custom MAIL FROM
   `mail.taptime.mt` (MX hatasında "Use default MAIL FROM").
3. Cloudflare DNS (`taptime.mt`): 3× CNAME `<token>._domainkey` → `<token>.dkim.amazonses.com`
   (**proxy KAPALI / DNS only**) · MX `mail` → `feedback-smtp.eu-central-1.amazonses.com` (10) ·
   TXT `mail` → `v=spf1 include:amazonses.com ~all` · TXT `_dmarc` →
   `v=DMARC1; p=none; rua=mailto:<rapor adresi>; adkim=r; aspf=r` (2–4 hafta temiz rapordan sonra
   `quarantine`, ardından `reject`). Apex'teki mevcut MX/SPF'e dokunma.
4. SES'te DKIM ve MAIL FROM "Successful" olana kadar bekle.
5. Yapılandırma seti `tappa-transactional`: TLS Require · açılma/tıklama izleme KAPALI · reputation
   metrics açık · kimliğe varsayılan set olarak ata · hesap düzeyi bastırma (BOUNCE+COMPLAINT) açık.
6. Sandbox testi için kendi adresini Verified identity olarak ekle.
7. Production access talebi: Transactional; `https://taptime.mt`; "işverenin davet ettiği
   çalışanlara tek kullanımlık aktivasyon linki ve panel yöneticilerine parola sıfırlama; düşük
   hacim, pazarlama yok"; bounce/complaint: hesap düzeyi bastırma + izleme.
8. IAM: SES → SMTP settings → Create SMTP credentials; oluşan IAM kullanıcısının politikasını daralt
   (yalnız `ses:SendRawEmail`; kaynak identity + config set ARN; `ses:FromAddress =
   no-reply@taptime.mt`; opsiyonel `aws:SourceIp`). SMTP kullanıcı/parolası BİR KEZ gösterilir →
   parola yöneticisine.
9. 🔴 SMTP kimliklerini sohbete, commit'e, ekran görüntüsüne YAPIŞTIRMA (A-0 dersi).
10. `tappa-secrets`'a `TAPPA_SMTP_USERNAME`, `TAPPA_SMTP_PASSWORD` ekle (`read -rs` + patch) —
    ConfigMap'teki `…_DELIVERY=email` değişikliğinin deploy'undan ÖNCE (yoksa yeni pod boot'u
    reddeder; `maxUnavailable: 0` eski pod'u tutar, deploy başarısız görünür).
11. CloudWatch alarmları: `Reputation.BounceRate > 0.02`, `Reputation.ComplaintRate > 0.0005`.
12. Gizlilik politikası + DPA alt işleyici listesine "Amazon Web Services EMEA SARL — SES,
    eu-central-1, işlemsel e-posta" (`/admin/legal` ya da OP-10 sonrası `/operator/legal`).
13. (Ops.) Cloudflare Email Routing: `support@`, `dmarc@` → kendi kutun.
14. Gerçek cihaz turu (EM-8).

## 5. Akış C — White-label (tenant markası)

### Karar: tek vurgu rengi + bir logo, co-brand — ✅ (tap ekranı ✅ D-C: logo + accent tap butonunda)
İstek (*"kendi logosu ve renkleri"*) CLAUDE.md §9'la iki yerde çelişiyor: "paletin dışına çıkma"
ve "tap ekranına özellik eklemek istiyorsan önce sor". Uzlaşma: tenant YALNIZ tanımlı slotları
boyar, durum renkleri asla değişmez. Tenant logosu önde, küçük "taptime · punchless" kalır (GDPR
metni Taptime'ı işleyen olarak adlandırıyor, plaket "taptime" basıyor).

### Tasarım özü (ADR 0023/0024'te normatif)
- **Renk:** tek accent, serbest hex, **WCAG kapısı** — saf `internal/brand/accent.go`:
  `ParseAccent` (kanonik büyük harf) · `OnColor` (ink ya da paper, yüksek kontrastlı olan) · `Edge`
  (porcelain'e karşı <3:1 → 2 px ink kenar, WCAG 1.4.11) · `Check` (en iyi metin kontrastı <4,5:1
  → RED) · `Suggest` (aynı ton, açıklığı düşürerek geçen en yakın renk). Red bandı bağıl parlaklık
  L ∈ (0,1790; 0,2368) — orada ne paper ne ink 4,5'e ulaşır (ör. #808080 ve #E0457B red; #FFC72C
  sarı ink metin + ink kenarla geçer; #DA291C kırmızı paper metinle 4,78 geçer). Aynı fonksiyon
  yazma VE okuma tarafında (netx deseni). Palet sabitleri `tailwind.config.js`'den türetilip
  testle kilitli (ikinci kopya yok).
- **Asla değişmeyen:** beş kaşe damgası + durum→renk eşlemesi · tomato = hata/yıkıcı · saffron =
  FLAGGED/geç · Notice · docket/perforasyon · `.docket-label` · panelin birincil/yıkıcı butonları ·
  focus halkası (ink) · **sonuç ekranında accent hiç yok** (renkler durumu anlatıyor).
- **Enjeksiyon — CSP DEĞİŞMEZ:** durumsuz `GET /brand/theme/{HEX}.css` → gövde yalnız
  `:root{--brand-accent:R G B;--brand-on-accent:…;--brand-edge:…}`; DB okumaz, kimlik doğrulamaz,
  tenant verisi taşımaz (kurucusuna havuz verilmez — `/healthz` gibi yapısal); yalnız kanonik ve
  kontrolden geçen hex 200, gerisi 404; `public, max-age=31536000, immutable`, nosniff.
  `style-src 'self'` zaten kapsıyor; `style=` / `<style>` / `'unsafe-inline'` YOK (bugün hiçbir
  politikada yok, landing turunda "0 `style=`" ölçülmüş). Tailwind'e `brand`, `on-brand`,
  `brand-edge` token'ları `rgb(var(--…) / <alpha-value>)`; `:root` varsayılanı tappa-green/paper →
  marka ayarlamamış tenant'ta computed style bugünküyle aynı.
- **Logo:** yalnız **PNG/JPEG** (SVG doğrudan açılınca script çalıştırır ve stdlib'de güvenli
  sanitizer yok; GIF animasyon; WebP `x/image` bağımlılığı). Saf `internal/brand/logo.go`
  `Normalize(io.Reader)`: gövde `MaxBytesReader` 1 MiB, parça ≤512 KiB · `http.DetectContentType`
  magic bytes (istemci Content-Type ve dosya adı yok sayılır, LOGLANMAZ) · format-özel
  `png.DecodeConfig`/`jpeg.DecodeConfig` ile **decode'dan ÖNCE** her kenar 16–2048 px ve ≤4 MP
  (decode-bomb; en kötü 16 MiB RGBA, 512 Mi pod içinde) · süreç geneli 1–2 slot semafor · uzun
  kenar 512 px'e elle box filtre (stdlib; `x/image/draw` bağımlılık olurdu) · PNG→PNG
  (BestCompression), JPEG→JPEG q85 **yeniden kodlama** → EXIF/XMP/ICC/metin chunk'ları ve IEND
  sonrası polyglot düşer — 🔴 **§4.2: telefon fotoğrafının EXIF GPS'i silinir (testle)** · çıktı
  >256 KiB → "logoyu sadeleştir" · `sha256(çıktı)` URL anahtarı (⚠️ "fingerprint" kelimesi R1
  tetikleyicisi — "digest"/"sha256" kullan).
- **Depolama: Postgres `bytea`** — pod `readOnlyRootFilesystem` + hiç volume yok, kümede obje
  deposu yok (`50-backup.yaml` ölçmüş), `pg_dump` yedeğine kendiliğinden girer; 256 KiB × 1000
  tenant ≈ 256 MB, TOAST satır dışında tutar. Yükleme `r.MultipartReader()` ile AKIŞ —
  `ParseMultipartForm` büyük parçayı geçici dosyaya yazmaya kalkıp salt-okunur FS'te patlar.
- **Veri:** ayrı `tenant_branding` tablosu (0..1 ilişki; `store.Tenant`'a bytea taşıma riski yok):
  `tenant_id` PK (tablo-kısıtı biçimi — R5b), `accent char(6)` CHECK `^[0-9A-F]{6}$`, `logo bytea`
  ≤262144, `logo_sha256`, `logo_mime IN ('image/png','image/jpeg')`, `logo_width/height` 1–512,
  `updated_at`, `updated_by` + `FOREIGN KEY (updated_by, tenant_id) REFERENCES admin_users (id,
  tenant_id)`, logo alanları hep-birlikte-dolu CHECK; ENABLE+FORCE RLS, NULLIF politikası; GRANT
  SELECT/INSERT/UPDATE, DELETE YOK (sıfırlama UPDATE … NULL). Değiştirilebilir (marka hukuki delil
  değil); geçmiş audit_log'da. sqlc: `GetTenantBrand` (logo byte'larını SEÇMEZ — testle),
  `GetTenantLogo(tenant_id, sha)`, `UpsertTenantAccent`, `UpsertTenantLogo`, `ClearTenantAccent`,
  `ClearTenantLogo` — hepsi açık `tenant_id` filtreli.
- **Servis:** `GET /admin/brand/logo/{sha}` (panel okuma zinciri) ve `GET /t/logo/{sha}` (tap
  grubu, canlı oturum şart) — admin çerezi `Path=/admin`, çalışan çerezi `Path=/` olduğu için tek
  ortak rota iki çerezi alamaz. Tenant YALNIZ oturumdan; başka tenant'ın logosu ve var olmayan hash
  **bayt-aynı 404** (kehanet yok). Başlıklar: saklanan mime, `private, max-age=31536000,
  immutable`, `ETag: "<sha>"`, nosniff, `Content-Security-Policy: default-src 'none'; sandbox`,
  `Cross-Origin-Resource-Policy: same-origin`, sabit `Content-Disposition: inline;
  filename="logo.png"`. Sayfa CSP'sine `img-src 'self'` YALNIZ `<img>` render edilen sayfaya
  (`tapCSPFor(hasLogo)` / `adminCSPFor(hasLogo)`, `landingCSPFor` emsali). Kimliksiz hash-rotası
  elendi (SECURITY DEFINER ile tenant'lar arası okuma = ADR 0002 md.7'ye yeni istisna + bütçesiz DB
  okuması).
- **Yüzeyler:** tap ekranı logo (sabit yükseklikli başlık yuvası) + accent tap butonunda (⏳ D-C)
  · sonuç ekranı yalnız logo · tur/practice logo (faz 2) · panel kabuğu logo + tenant adı + 4 px
  accent şeridi (birincil butonlar yeşil kalır) · Account → "Your brand" editör + gerçek bileşen
  önizlemesi (mevcut "What your staff read" deseni) · aktivasyon (Taptime + işveren adı, bugünkü
  gibi) / problem / landing / legal / signup / admin login / reset / operatör / AdminChoose =
  Taptime · CSV değişmez · e-posta faz 1 yalnız ad (sabit Taptime gönderen), logo faz 2 CID (≤32
  KiB varyant; uzak URL = takip pikseli, elendi). Plaket/oturum tenant uyuşmazlığında
  (`ErrForeignLocation`, `tap.go:387-395`) Taptime varsayılanı.
- **Yetki/audit:** owner-only (`mayEditAccount` yeniden kullanılır); `tenant.brand_updated` aynı
  tx `RecordTx`, detail'de sabit 6 anahtar (`field`, `before`, `after`, `bytes`, `width`,
  `height` — görsel byte'ı ve dosya adı YOK); reddedilen deneme `tenant.brand_update_refused`.
  **§4.6:** marka okuma hatası hiçbir sayfayı düşürmez → varsayılana düşer + log (`PendingBadge`
  deseni); marka yoksa ek `<link>`/`<img>`/`img-src` yok, HTML bayt-aynı.
- **§4.1:** logo bir kişinin fotoğrafı olabilir ama biyometrik işleme yapılmaz; yüz tespiti
  EKLENMEZ (kendisi biyometrik işleme olurdu). Form metni "a logo, not a photo of a person".
- **Kabul edilen risk:** marka taklidi (kendi kendine kayıt olan tenant başka işletmenin logosunu
  yükleyebilir; plaket uyuşmazlığı `sys:tenant-mismatch` ile yakalanır) → ADR 0005 append.

### Görevler
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| WL-0 | Kararlar (D-C + WL-K*) open-questions'a cevaplı + ADR 0023/0024 + tappa-brand skill'e "Tenant slotları" taslağı | S | orkestratör | kararlar kayıtlı; ADR'ler kabul | D-C |
| WL-1 | Migration `tenant_branding` + `db/queries/branding.sql` + RLS testi | M | tappa-db-migrator | `make audit` R5/R5b 0; beşli tam; RLS testi `WHERE`'siz A bağlamında B'yi 0 görür + B `tenant_id`'li INSERT WITH CHECK ile red; `has_table_privilege('tappa_app','tenant_branding','DELETE')=false`; CHECK'ler hasmane değerlerle patlatılmış (küçük harf hex, 3 haneli hex, 262145 bayt, kısmi logo alanları); Down→Up→Down bayt-aynı; `GetTenantBrand` `logo` seçmiyor (test sorgu metnini okur) | WL-0 |
| WL-2 | `internal/brand/accent.go` | S | builder | tablo değerleri ±0,01, L sınırları 0,1790 ve 0,2368 dahil; palet `tailwind.config.js`'den pinli (renk değişirse kırmızı); `Suggest` deterministik, her çıktısı `Check`'ten geçer (özellik testi); kapsam ≥%90 | WL-0 |
| WL-3 | `internal/brand/logo.go` | M | builder | SVG/GIF/WebP/HTML/PNG-magic'li HTML red; 30000×30000 başlıklı dosya decode'dan ÖNCE red (bayt başına tahsis ölçülür); kesik red; IEND sonrası yük çıktıda yok; EXIF-GPS'li JPEG çıktısında `Exif` APP1 yok; CMYK JPEG, 16-bit, paletli, interlaced PNG normalize; ≤512 px, ≤256 KiB; `FuzzNormalize` panik yok, her başarılı çıktı yeniden decode olur ve sınırlarda; semafor -race altında ≤N; `go.mod` diff boş | WL-0 |
| WL-4 | Domain `internal/domain/tenant/brand.go` | M | builder | kaydet/sil UPDATE + audit aynı tx (zorla patlatılan audit UPDATE'i geri alır; iki yön); detail tam 6 sabit anahtar; domain accent'i yeniden `Check` eder → `ErrAccentIllegible`; `ActorID` zorunlu | WL-1,2,3 |
| WL-5 | Theme rotası + Tailwind token'ları (`brandtheme.go`, `tailwind.config.js`, `input.css`) | M | builder + tappa-brand | kurucu havuz almıyor; 200/404 matrisi (kanonik, küçük harf, geçersiz, red bandı); başlıklar birebir, gövde yalnız 3 özellik; marka yoksa tap butonu CDP computed `rgb(31,92,65)`/`rgb(255,253,244)`; yeni bir marka testi: tenant accent'i yalnız marka slotlarında (henüz yazılmadı — adı WL-5'te konur; mutasyon: `.stamp`'e `bg-brand` → kırmızı); `TestCompiledCSS_StampWordIsInk` yeşil; yorumdan ölü CSS kuralı doğmuyor | WL-2 |
| WL-6 | Logo rotaları + sayfa başına `img-src` | M | builder | başlıklar birebir; A oturumu B'nin sha'sını isteyince aldığı 404 bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu 404; "sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır" testi (panel + tap); ücretli istek sayısı ölçülüp bütçeler güncellendi (sıcak 1, soğuk 2) | WL-1,4 |
| WL-7 | Account → "Your brand" editörü (`brandactions.go`, account.templ/view, üç `ProtectWriting` rotası: logo, accent, sıfırla) | L | builder + tappa-brand | yükleme `MultipartReader` akış, `TMPDIR` salt-okunur/yokken bile başarılı (geçici dosya yok); yalnız tek `logo` parçası, bilinmeyen parça red; cross-origin POST resolver'dan önce red; manager POST 303 `not-permitted` + `brand_update_refused` + 0 UPDATE; okunaksız renk formu yeniden render + önerilen hex, yazma yok; önizleme gerçek tap bileşenlerini render eder; açık logo uyarısı (alfa ağırlıklı parlaklık paper'a <1,5:1 → uyarı, ret değil); `FactNoBulkImport` tripwire'ı "marka logosu handler'ı dışında multipart okuyucu yok" olarak yeniden türetildi (mutasyon: `employeeactions.go`'ya `FormFile` → kırmızı; SSS cümlesi değişmez); `<input type="color">` + hex alanı dokunma hedefi ≥44 px | WL-4,5,6 |
| WL-8 | Panel kabuğu (`panelChrome`, `PanelChrome`, `review.go` `chrome()`) | S | builder | logo + tenant adı + şerit (K4); `chrome()` tam +1 PK okuması, EXPLAIN ANALYZE seed'de <1 ms; marka okuma hatası sayfayı düşürmez; AdminChoose değişmez | WL-5,6, OP-10 |
| WL-9 | Tap, sonuç, tur ekranları (`base.templ` açık parametreli `BrandedPage…`, `tap.templ`, `result.templ`, `view.go`, `tap.go`/`checkin.go` `tapCSPFor`, `directory.go` `TapPage` markayı aynı tx'te okur, `result_test.go` beyaz listesi) | M | builder + tappa-brand | **§9 onayı (D-C) olmadan başlamaz**; `TapView` 3 ve `ResultView` 8 alan kalır (marka ayrı açık parametre; kartta onay alıntılı); logo `width`/`height` → CDP layout-shift 0; 390×844'te tap butonu üst kenarı ≤16 px kayar, hâlâ tek buton, "Tap" metni, ≥64 px; tek yeni metin `alt` = tenant adı; A çalışanı B plaketinde gövdede `/t/logo/` yok; marka yoksa HTML + CSP bayt-aynı (golden); okuma hatasında 200 + varsayılan | WL-5,6, D-C |
| WL-10 | Güvenlik denetimi (tüm WL diff'i) | M | tappa-security-auditor | ONAY — izolasyon (RLS + handler), fuzz/bomb, başlıklar, CSP diff, multipart tripwire, bütçeler, audit, log'da dosya adı/byte yok | WL-7,8,9 |
| WL-11 | E-posta entegrasyonu | S | builder | `"X\r\nBcc: y"` tenant adı ek başlık üretmez; RFC 2047; From alan adı hep Taptime | EM-7, WL-4 |
| WL-12 | Dokümanlar (tappa-brand skill tenant slotları + kontrast kuralı, handoff §4, roadmap, state) | S | orkestratör | `make check` + `make audit` exit 0 | WL-10 |

Bilinçli güncellenecek mevcut testler: `TestResultScreen_SaysExactlyThisAndNothingElse` (`alt`
metni), `FactNoBulkImport`, `TestBrand_*`, panel CSP ↔ script karşılığı testi.

## 6. Kararlar

**✅ Kullanıcı kararları (2026-09-24):**
| # | Soru | Öneri | **Karar** |
|---|---|---|---|
| D-A | Depo PUBLIC — ne yapılsın? Scrub push'u? | private yap + push | **Public kalsın, yalnız push.** Sonuç: geçmişteki değeri öldüren tek önlem F0-1 (rotate); F0-5 sır kapısı kritik hale geldi — depo herkese açık kaldıkça her commit yayındır |
| D-B | Süper admin modeli | (c) hibrit | **(c) ayrı operatör kimliği** — TOTP + `ops.taptime.mt` + `op_*` definer'lar; önceki "B — allow-list genişlet" kararının yerine geçer |
| D-C | Tap ekranı markası (§9) | logo + accent tap butonunda | **Logo + tap butonu tenant renginde**; sonuç ekranında yalnız logo (§9 onayı — WL-9 kartına alıntılanır) |
| D-D | Akış sırası | Faz 0 → A1 → B → C → A2 | **Faz 0 → Süper admin (A1) → SES (B) → White-label (C) → A2**; SES dış adımları paralel |

**⏳ Sırası gelince sorulacak:** OP-K5 askıdaki ayın faturalanması · EM-K9 Mailpit (yeni dev aracı,
docker-compose, Go bağımlılığı değil) · OP-K12 T27/T37 (plan geçmişi, OP-17 öncesi) · OP-K2/K4
DNS + IP kısıtı ayrıntısı (D-B onaylanırsa).

**✅ Önerisiyle uygulanır** (otonomi kuralı; itiraz edilirse değişir):
- **Operatör:** K3 TOTP zorunlu (kurtarma CLI ile, kurtarma kodu yok) · K4 IP kısıtı opsiyonel ·
  K6 değiştiren eylemler tenant audit'inde de görünür, okumalar yalnız operatör log'unda · K7
  impersonation YOK · K8 encode Faz 4'te operatöre · K9 süreç ayrımı şimdilik yok · K10 operatör
  parolası ≥14 (tenant 8 kalır) · K11 geçişten sonra KF hesabı sıradan müşteri.
- **E-posta:** K1 `net/smtp` · K2 akış başına config · K3 adresi olmayan çalışan için owner-only
  "linki göster" yedeği · K4 yalnız EN · K5 sabit Taptime gönderen · K6 `mail.taptime.mt` · K7
  eu-central-1 · K8 bounce/complaint pilot sonrası · K10 SES TLS Require · K11 signup doğrulaması
  ayrı / pilot sonrası · K12 "parolanız değişti" bildirimi evet · K13 panel yedeği kalır.
- **White-label:** K1 tek accent + WCAG kapısı · K3 co-brand · K4 panelde 4 px şerit · K5 PNG/JPEG
  · K6 aktivasyon Taptime + işveren adı · K7 Postgres bytea · K8 e-postada faz 1 yalnız ad.

## 7. Kapsam dışı / riskler (özet)
- **Kapsam dışı:** tenant'a özel alan adı/subdomain (istenmedi; SUN URL'i çipte encode, `cookies.go`
  subdomain uyarısı) · tenant yazı tipi · karanlık tema · markalı fiziksel plaket · favicon/PWA ·
  impersonation · çoklu operatör rolleri · WebAuthn · kurtarma kodları · GDPR silme aracı (ayrı
  ADR) · rapor e-postası · Q28 uyarı teslimi · SMS · BIMI/MTA-STS · ayrı Deployment.
- **Riskler:** tek operatör tüm platform gücünü taşır (kontrol yalnız audit; iki kişi kuralı kapsam
  dışı) · TOTP gerçek zamanlı relay oltalamasına açık (WebAuthn sonraki iş) · aynı süreçte RCE iki
  DSN'i de ele geçirir (K9) · elle tiplenmiş definer çağrıları SQL'den sapabilir (testler sevk
  edilen SQL'i koşturur) · canlı kümede rol/Secret kurulumu kullanıcı işi (T44/T45 emsali) · iOS
  uygulama-içi tarayıcı aktivasyonu (EM-8 turu şart) · SES production access gecikmesi/reddi ·
  Hetzner 25/465 engeli (587 kullanılır, ilk gönderimde doğrulanır) · bastırılmış adrese gönderim
  sessiz (SNS yokken) · beyaz logo açık zeminde kaybolur (uyarı var) · soğuk önbellekte ücretli
  istek +1 · `/static` bugün cache/nosniff başlığı göndermiyor (ayrı sertleştirme işi).

## 8. Çalışma disiplini (değişmedi, bir ekle)
Her kod görevi: dal → yapıcı (opus) → her turda YENİ üçüncü göz → §4'e değince AYRICA
tappa-security-auditor, UI'ya değince tappa-brand; denetçiler sıralı; onay gelmeden done/commit
yok; commit öncesi `gofmt -l` + `make gen` + `make check`'in fmt/gen/diff yarısı (T72 kırmızısı
bunu atlamak için gerekçe DEĞİL — 25. oturumda CI'yı kırdı); **YENİ: commit öncesi sır taraması —
hiçbir dosyaya sır DEĞERİ yazılmaz (F0-5, A-0 dersi)**; main'e birleştirme = deploy = kullanıcı
kararı.
