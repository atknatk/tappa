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
| F0-1 | Operatör panel parolasını değiştir (Account → Change password, T73). Yeni parola sohbete/repoya yazılmaz | **Kullanıcı** — sahiplendi (2026-10-01): zamanı gelince kendisi yapar, plan takip etmez | eski parolayla giriş 401; diğer oturumlar iptal (K3) |
| F0-2 | Scrub commit'ini push et | orkestratör (kullanıcı onayıyla) | `origin/main`'de değer 0 |
| F0-3 | Depo görünürlüğü — ✅ **karar: PUBLIC kalır** (D-A). Geçmişteki değer yalnız F0-1 ile ölür | **Kullanıcı** | — (karar verildi) |
| F0-4 | `tappa_app` + `tappa_owner` DB parolalarını rotate et (tappa-secrets + `ALTER ROLE`) | **Kullanıcı** (§4.7: ajan tappa-secrets'a dokunmaz) — sahiplendi (2026-10-01): zamanı gelince kendisi yapar, plan takip etmez | `/readyz` 200; eski parolayla bağlantı reddi |
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
| OP-6 | `internal/operatorauth` (parola, TOTP, oturum, çerez, token, limiter/kilit) | L | builder + güvenlik | RFC 6238 Ek B vektörleri; aynı kod N goroutine → tam 1 başarı (-race); yanlış KEK açamaz; sızıntı testleri harici pakette; 8 sa / 30 dk / MFA'sız / revoked / disabled hepsi red (saat enjekte DB testleri) | OP-5 |
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
>   → **OP-6'da kapananlar (2026-09-26, doğrulama 2026-09-30; gerekçe OP-6 kart düzeltmesi
>   md. 1):** (b), (c), (d) OP-6'ya çekildi — ifadeler `internal/db/operator.go`'da `OperatorConn`
>   alan serbest fonksiyonlar, belgesi `db/queries/operator.sql`; (b)'yi
>   `TestOperatorSQL_OnlyBoundParameters`, (c)'yi `TestOperatorErr_NeverCarriesAPgError` pinler.
>   **OP-7'ye kalan:** `db.OperatorDB` tipi (havuz, rol ölçümü ve `roleRefusal`, `WithTenant`
>   yokluğu; `OperatorConn`'u karşılar ve `operatorauth.Store`'u bu fonksiyonlara devrederek
>   uygular) · (a) başlangıç parametresi pini (`log_parameter_max_length_on_error = 0`) ve
>   açılışta 0 değilse ret · config ve dört değişken · anahtarların diğer her anahtardan farklı
>   olmaması halinde açılış reddi · `operatorauth.New`'a anahtarların (`TOTPKEK`,
>   `TokenHMACKey`) ve logger'ın verilmesi. **Test kuralı (OP-6 md. 16):** operatör tablolarına
>   dokunan her yeni DB testi `tappa/test/operator-tables` danışma kilidini **paylaşımlı** alır
>   (`internal/db`'nin `opTx`'i özel alır; almazsa `go test ./...` iki paketi paralel koşturup
>   00026 DDL testlerini 40P01/55P03 ile düşürür — ölçüldü). → **OP-6 6. turdan (2026-09-30),
>   adıyla:** `*operatorauth.Authenticator` ve `operatorauth.Config` artık kendini redakte eder
>   (beş yöntem — ADR 0020 §2 notu). ~~OP-7'nin config wiring'i anahtarları redaksiyonsuz bir
>   yapıya kopyalamaz~~ *(8. turda daraltıldı — aşağıdaki karar: kural `operatorauth` tarafı
>   içindir)*. **8. tur, orkestratörün kararı (çelişki giderildi):** `internal/db/pool.go`
>   `internal/config`'i import eder, yani `config` `operatorauth`'u import edemez ve
>   `config.Config` bir `Key` tutamaz. Bu yüzden:
>   (1) operatör anahtarları (`TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_TOKEN_HMAC_KEY`)
>   `config.Config`'te öteki anahtarlar gibi (`TagKEK`, `SessionHMACKey` …) HAM `[]byte` olarak
>   durur ve "diğer her anahtardan farklı" açılış reddi orada, ham değerlerde koşar
>   (`config.keySeparation` emsali); `config.Config`'in kendi redaksiyonu repo genelinde ayrı bir
>   iştir (orkestratörün backlog maddesi, `TagKEK`/`SessionHMACKey` ile aynı);
>   (2) `Key`'e dönüşüm `cmd/tappa`'nın wiring'inde, `operatorauth.NewKey(b)` ile yapılır (kopya
>   alınır, çağıran silebilir); `Key`'in dışa açık erişimcisi yoktur ve gerekmez;
>   (3) 6. turun kuralı `operatorauth` TARAFINDA geçerlidir: `Key`'e çevrildikten sonra değer
>   redaksiyonsuz bir yapıya kopyalanmaz, `operatorauth.Config` bir yapının dışa kapalı alanında
>   değer olarak tutulmaz; süreç boyunca tutulan `*Authenticator`'dır. `Key` kendini redakte eder
>   ve baytları `*string` arkasındadır: bir `Config` değeri her yolda — dışa kapalı alanda,
>   `%p`, `%w` dahil — anahtarı basmaz (7. tur, ölçüldü: sızıntı testinin matrisi).
>   (4) **`db.OperatorDB` DSN'i ya da parolayı düz bir alanda tutmaz; havuz yalnız işaretçiyle
>   tutulur** (ölçüldü — 9. turda 8. denetçinin ölçümüyle yeniden yazıldı: `store`'un DÜZ bir
>   alanı, `Authenticator` değer ya da işaretçi olarak tutulsun, basılabilir — fiil listesi
>   yazılmaz, çünkü her tutuluş başka bir alt kümede basar; `*string` alan hiçbir fiilde
>   basılmadı. Kural: düz alan yok).
>   → **OP-7'de uygulandı (2026-10-01):** yukarıdaki listenin hepsi ve T79 *(2c: T79'un ve
>   ADR 0021 "Karar verilmedi" (1)–(2)'nin "uygulandı" iddiası, pgx'in istemci tarafı gömme
>   kipinin reddiyle BİRLİKTE doğrudur — OP-7 kart düzeltmesinin "2c" satırı)*; sapmalar ve
>   ölçümler aşağıdaki *"Kart düzeltmesi (2026-10-01, OP-7 uygulaması sırasında)"* bloğunda
>   (en önemlisi md. 1: `OperatorDB` kendisi bir `OperatorConn` DEĞİL).
> - **OP-8:** oturum kapısı `op_touch_session`; giriş sonu `op_open_session`, çıkış
>   `op_close_session`; **enrollment handler'ı `op_complete_enrollment`'a bağlı** (B10);
>   her okuma ekranı önce `op_begin_read`'i ayrı transaction'da commit eder; `pending`
>   hesap *"aynı yanıt, aynı süre"* kümesinde; `POST /operator/enroll` oran sınırı
>   (sınır 12); limiter testi (OP-6 ile). → **OP-6'dan gelenler (2026-09-26, doğrulama
>   2026-09-30):** `floodGate` = `Authenticator.AllowRequest` (adres başına 300/10 dk); `TOTP`
>   adres almaz, `Password` ve `CompleteEnrollment` adresi yalnız `work` bütçesi için alır (OP-6
>   md. 17 API notu); `enroll` bütçesi (süreç geneli 10/10 dk) OP-6'da **öneri** olarak sevk
>   edildi, OP-8 aritmetiğiyle değiştirebilir (OP-6 md. 8) — ⚠️ **ölçüldü (2026-09-30, 2. tur):
>   TEK bir adres bu bütçeyi tüketip her adresten enrollment'ı reddettirir — ve 3. turda
>   ölçüldüğü üzere pencere pencere SÜRESİZ; yeni link kaçış değil; her istek bir
>   `enrollment_failed` satırı yazar** — *(4. tur düzeltmesi: mekanizma sabit pencere; sayaç
>   pencere dolunca kendiliğinden sıfırlanır, yani sürekli ret her pencerenin başında 10'luk bir
>   PATLAMA ister; ~~dakikada ~1 istek~~ eşit yayılmış istekler bütçeyi pencerenin çoğunda açık
>   bırakır; ~~sayacı yalnız süreç yeniden başlatması sıfırlar~~ — sıfırlanma pencerenin kendisi)*
>   (`work` 20/adres > `enroll` 10/süreç; `BeginEnrollment` DB okumadığı için saldırgan kendi
>   sayfasını açıp geçerli ilk kodu yazabilir) — ~~adres başına pay OP-8'in kararı (OP-6 md. 8'de
>   öneriyle)~~ **12c: adres başına pay sevk edildi** (`enrollAddr` 3/10 dk, `enroll`'dan önce;
>   OP-8 aritmetiğiyle değiştirebilir). **OP-8'e, adıyla:** dağıtık saldırgan (≥4 hız anahtarı;
>   IPv6'da RateKey bir /64, bir /48 sahibi 65 536 anahtar tutar) süreç bütçesini yine tüketir —
>   çare OP-8'in (operatör yüzeyinde ops IP kısıtı, K4, ya da başka bir önlem). **OP-8/OP-14'e,
>   adıyla (12c):** doğru parolası girilip TOTP'si tamamlanmayan giriş bugün yalnız bir slog Info
>   satırı bırakır (`operator first factor verified; second factor pending` + `operator_id`); kalıcı
>   bir `password_ok` audit türü 00026'nın kapalı tür kümesine bir migration ister — OP-8/OP-14'ün
>   kararı. Form sınırındaki adres doğrulaması (UTF-8, NUL, uzunluk) reddi parola adımının
>   aynı `ErrRefused` yolundan vermeli (OP-6 md. 8, saklanamayan adres). **İstemci adresi
>   (OP-6 4. tur):** CLAUDE.md §7'nin "asla loglanmaz" listesinde yok; `operatorauth` onu yalnız
>   bütçe anahtarı olarak kullanır ve hiçbir satıra, hataya ya da log'a yazmaz (sızıntı testi
>   adresleri arama kümesinde tutar) — handler'ın log ve hata yüzeyinde adresin nasıl ele
>   alınacağı OP-8'in kararıdır, adıyla. **OP-8'in kendi sızıntı testi** OP-6'nın iki dersiyle
>   yazılır: yakalama **Debug** seviyesinde, üretimin iki handler'ıyla (text ve JSON), ve iddia
>   **numaralı bir sözleşmedir** — üye GRUPLARI numaralı ve kaynağıyla; **kapalı ölçüt**
>   CLAUDE.md §7 + ADR 0020 §5 + ADR 0021 §3.5'in "asla loglanmaz" listesidir: her MADDENİN
>   bağlı olduğu her grupta en az bir üyesi olmak zorunda, test bunu sayar. ~~her grup kapalı
>   bir ölçüte bağlı~~ *(6. tur düzeltmesi: yanlıştı — emsalde G16, istemci adresi, bilerek
>   hiçbir maddeye bağlı değil, paketin kendi iddiası olarak aranır; bağlılık maddeden gruba
>   doğrudur)*. RENDER'LAR numaralı ve her biri adıyla eşlenmiş kendi builder'ıyla pozitif
>   kontrollü (6. tur: eşlemesiz bir render — emsalde R3 — boşaltılınca yeşil kalıyordu); KOLLAR
>   numaralı; aranmayanlar ADIYLA (bölünmüş/kısmi değerler; listede olmayan render'lar —
>   ölçülüp sayılarak; dışarıdan başarısız kılınamayan kollar); hasat (sahte store'un aldığı
>   değerler) metot başına pinli — **sayı ve arite, içerik değil** (sayılı sınır); üye başına
>   pozitif kontrol. **Tip kümesi kapalıdır** (6. tur; 7. turda düzeltildi): paketin ve
>   `internal/db`'nin test dışı kaynakları tip-denetlenir ve paketin dışa açık her bildiriminden
>   (tip, fonksiyon, değişken, sabit) dışa açık alanlar, dışa açık yöntem imzaları ve arayüz
>   yöntemleri üzerinden ulaşılan her adlı tip — dışa açık bir yöntemin döndürdüğü dışa kapalı
>   tip dahil; ~~*8. turda:* bu bildirimlerin METNİNDE adı geçen her tip de …~~ **9. turdan:
>   tipler KESİN** — `go list -export -deps` derleyicinin export verisini adlandırır,
>   `go/importer.ForCompiler(…, "gc", lookup)` okur; böylece takma ad, generic örneğinin TİP
>   ARGÜMANLARI ve çıkarımla tipi gelen dışa açık değişken kendiliğinden çözülür. **Neden bu
>   yol (OP-8 aynısını kopyalayacaksa):** 7. ve 8. turda dar denetimin sözdizimiyle kovaladığı
>   kör nokta iki tur üst üste yeni bir biçimle geri geldi; kesin yükleme ölçüldü — testin
>   kendisinde sıcak önbellekte 0,23 sn (`-race` 0,35–0,39 sn), soğuk önbellekte 0,58 sn
>   (`-race` 0,95 sn) — ve "göremediğini yasakla" yolu 8 sentinel hatayı (`Err… =
>   errors.New(…)`) adıyla istisna yapmayı gerektiriyordu. **10. turdan:** kapanış bir
>   öncüle değil YAPIYA bağlı — yürüyüş, go/types'ın tip grafiğini TÜKENMİŞ bir anahtarla tam
>   dolaşır (her tür adıyla; tanınmayan tür = kırmızı), hangi paketin olursa olsun, her struct
>   alanı dışa açık ya da kapalı, her dışa açık yöntem, kısıtlar ve union terimleri; ~~kayıt
>   yalnız iki paketin adlı tipleriyle sınırlı~~ *(11. turdan: kayıt modülün HER paketinin adlı
>   tipleri — `func E() sun.EV2Auth` dersi; alan yürüyüşü her paketin yapısına girer; alan
>   girdileri TİPİYLE pinli; specimen'in aradığı sırlar tuttuğu redakte değerlere karşı mekanik
>   olarak denetlenir)*.
>   9. turun "başka paket yalnız tip argümanıyla" öncülü yanlıştı (`sun.Result` →
>   `db.ResolvedTag`). ~~Kalan sayılı sınır yalnız çalışma zamanında doldurulan `any` ve
>   reflection~~ *(11. tur: sayılı sınırlar OP-6 md. 18'in **tek listesinde** — P1–P9,
>   S1–S14 (12c: P8, P9); OP-8 kendi listesini aynı biçimde tek yerde tutar. 12. turun dersi, OP-8 aynısını
>   kopyalayacaksa: bir istisnanın GEREKÇESİ bir iddiadır ve test onu doğrulamaz — dört tur
>   üst üste bir gerekçe, test etmediği bir şeyi iddia etti; kapanış YAPIYLA: alan kuralı
>   istisnasız (her alan adıyla ve tipiyle, eklemek/silmek/tip değiştirmek kırmızı), muafiyet
>   yalnız ÖLÇÜLMÜŞ bir yazdırma davranışına dayanır ve o ölçüm bir testtir, gerekçe metni
>   gözden geçirenin iddiası olarak ilan edilir)*) — ya doldurulmuş bir örnekle **ölçülen
>   matriste** (fmt'nin bütün fiilleri × altı
>   biçim × `Sprintf`/`Errorf` + slog + `json.Marshal`) taranır ya da adıyla ve gerekçesiyle
>   istisnadır; kökler yalnız paketin kendi bildirimleridir (bir istisna kendini ulaşılabilir
>   kılamaz); fonksiyon içinde tanımlanmış tip reddedilir; sınıflandırılmamış yeni tip ya da
>   ulaşılamayan istisna kırmızı (emsal
>   `TestExportedTypes_EveryOneIsASpecimenOrANamedException`, alan düzeyinde
>   `TestExportedTypes_CarryNoPlainStringField`). **(a) redacting-tip önerisi (OP-6 5. tur,
>   ölçüldü):** handler'dan `operatorauth`'a düz `string` giden kimlik bilgileri — `Password`'ün
>   e-postası ve parolası, `TOTP`'un kodu, `CompleteEnrollment`'ın ham token'ı, parolası ve kodu
>   — OP-8'in handler parametrelerinde sarmalayıcı tipe alınabilir; kazara biçim fiilini kapatır
>   (D1, D2, D5, D7, D8 sınıfı), bilerek çıkarmayı kapatmaz (D4/D4b/D9 bugün zaten sarmalayıcı
>   olan `SessionToken`'dan `reveal()` ile sızdı) — kara kutu aramasının yerine değil, önüne. *"Türetilir"*, *"her kimlik
>   bilgisi"*, *"hiçbiri görünmez"* gibi evrensel sözcükler YAZILMAZ — OP-6'da dört tur üst üste
>   her biri kapsamı aşan bir değer ya da render buldu. Emsal
>   `internal/operatorauth/leak_external_test.go` (`TestLeak_NoInputInAnyErrorOrLogLine`'ın
>   başlığı). **Test kuralı (OP-6 md. 16):**
>   operatör tablolarına dokunan her yeni DB testi `tappa/test/operator-tables` danışma kilidini
>   **paylaşımlı** alır.
> - **OP-9:** `opadmin create` hesabın id'sini kendisi üretir ve enrollment linkine
>   token'la birlikte koyar; **token sorgu dizgisinde taşınmaz** — fragment ya da yol
>   parçası, ingress'in ne log'ladığı ölçülür; **`reset-mfa`** (zarfı siler, sayacı
>   sıfırlar, `pending`, oturumları iptal eder, yeni token + id'li link) — açık iş, adıyla.
>   → **OP-6'dan gelen biçim şartı (2026-09-26, işaretçi 2026-09-30):** token
>   `operatorauth.NewEnrollmentToken` ile basılır — 256 bit, **43 karakter base64url** (dolgusuz);
>   `CompleteEnrollment` başka biçimdeki bir token'ı `op_complete_enrollment`'a göndermeden ve
>   digest ödemeden reddeder — ~~veritabanına gitmeden~~ *(6. tur düzeltmesi: veritabanına
>   GİDER — süreç geneli audit tavanının altında bir `enrollment_failed` satırı yazılır,
>   `op_record_auth_event` üzerinden)* —, yani OP-9 başka biçimde basarsa **kimse enroll
>   olamaz** ve her deneme bir red satırı bırakır. Hash
>   `EnrollmentToken.Hash()` / `operatorauth.EnrollmentTokenHash` ile yazılır (anahtarsız SHA-256,
>   linkteki METNİN UTF-8 baytları, küçük harf hex — 00026'nın `encode(sha256(convert_to(…)))`'ü;
>   `TestEnrollmentTokenHash_IsTheDatabasesHash` canlı sunucuya pinler). Ayrıntı: OP-6 kart
>   düzeltmesi md. 7. `enroll_issued_at`/`enroll_expires_at`'in tek `clock_timestamp()`
>   okumasından yazılması kuralı OP-5 md. 27'de.
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
>     `02-dev-only-password.sh` emsaliyle, ~~üretim parolası bir pod'a `secretKeyRef` ile~~ →
>     *OP-7'de başka yoldan çözüldü: parola psql `\password` ile stdin'den, ayrı Secret anahtarı
>     yok — OP-7 kart düzeltmesi md. 8* —
>     ⚠️ `10-postgres.yaml`'a `tappa-secrets`'ta henüz olmayan bir anahtar için
>     `optional` olmayan bir `secretKeyRef` eklemek Postgres pod'unu başlatamaz); ADR 0021
>     §2 vi'nin `db/queries/operator.sql` belgesi ve `internal/db/operator.go` erişimcileri
>     (OP-5'te Go erişimcisi yok, bu yüzden yazılmadı). → *(2026-09-30 notu: belge ve erişimciler
>     OP-6'da yazıldı — OP-6 kart düzeltmesi md. 1; OP-7'ye kalan yukarıdaki OP-4 bloğunun OP-7
>     maddesinde.)*
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
> 30. **D8 ·** `db/queries/operator.sql` OP-7'de (değişmedi). → *(2026-09-30 notu: OP-6'da
>     yazıldı — OP-6 kart düzeltmesi md. 1.)*

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

> **Kart düzeltmesi (2026-09-26, OP-6 uygulaması sırasında).** Yazıldı: `internal/operatorauth/`
> (`operatorauth.go`, `password.go`, `totp.go`, `token.go`, `challenge.go`, `enrollment.go`,
> `cookie.go`, `limits.go`, `flow.go` + testler), `internal/sun/keys.go`'ya genel `Seal`/`Open`
> (+ `seal_test.go`), `internal/db/operator.go` + `db/queries/operator.sql` (+ `operator_test.go`),
> iki envanter güncellemesi (`cmd/tappa/constanttime_test.go` +2 dosya,
> `cmd/tappa/storekeyshape_test.go` 17 → 18 `.sql` dosyası), OP-5'in test yardımcısı `opTx`'e
> bir danışma kilidi (md. 16), ADR 0020/0021'e tarihli OP-6 notları. Migration YOK. Ölçüm: dev Postgres
> 17.10; DB testleri `tappa_owner` bağlantısından `SET LOCAL SESSION AUTHORIZATION tappa_operator`
> ile, **üretim erişimcilerinin kendisini** çağırarak (sahte depo yok). Sapmalar ve kararlar:
>
> 1. **(d) OP-7'den OP-6'ya çekildi — ölçülerek.** İki yol sayıldı: (A) OP-6 testlerinde yerel bir
>    `Store` uygulaması → OP-7 aynı yedi ifadeyi (iki giriş araması + beş `op_*` çağrısı) ve
>    28000 eşlemesini üretimde **ikinci kez** yazar, ve OP-6'nın bütün DB testleri sonsuza dek
>    üretimin değil testin kopyasını sınar (M6-09 B'nin "ikiz sözleşmeden ayrışır" sınıfı);
>    (B) ifadeler şimdi `internal/db/operator.go`'da → **0** kopya: testlerin eklediği tek şey
>    kimliktir (`SET LOCAL SESSION AUTHORIZATION`). (B) seçildi. Biçim: `*DB` üzerinde metot
>    DEĞİL, bir `OperatorConn` (Exec + QueryRow) alan serbest fonksiyonlar — `*DB` `tappa_app`'in
>    havuzudur ve orada her biri 42501 ile düşer (`TestOperatorAccessors_TheCustomerRoleCannotUseThem`).
>    OP-7'nin (b) ve (c) şıkları SQL'le birlikte buraya geldi ve pinlendi:
>    `TestOperatorSQL_OnlyBoundParameters` (her ifade sabit; tek tırnaklı literal yalnız şema
>    sabiti `'active'`; `$n` sayısı = argüman sayısı; her sabit `operator.sql`'de birebir) ·
>    `TestOperatorErr_NeverCarriesAPgError` (`*pgconn.PgError` hiçbir dönüşten `errors.As` ile
>    erişilemez; yalnız SQLSTATE kalır). **OP-7'ye kalan:** `db.OperatorDB` tipi (havuz, rol
>    ölçümü, `roleRefusal`, `WithTenant` yokluğu), (a) başlangıç parametresi, config ve
>    `operatorauth.New`'a anahtarların verilmesi; metotları bu fonksiyonlara devreder.
>    Giriş araması bir `-- name:` sqlc sorgusu yapılmadı: `platform_admins` tenant'sızdır ve
>    `tappa_app`'in store'u o rolle hiç koşmaz.
> 2. **Mühür:** `sun.Seal(kek, aad, pt)` / `sun.Open(kek, aad, env)` — `keys.go`'da, `aead()`'i
>    yeniden kullanarak (anahtar-programı tahsis envanteri `keys.go: 2` DEĞİŞMEDİ, silme-kapısı
>    bölümlemesi değişmedi). Düzen `Wrap`'ınki: `nonce(12) ‖ ct ‖ tag(16)`, sürüm baytı yok →
>    160 bit sır **48 bayt** (00026 tabanı 44). Boş AAD ve boş düz metin reddedilir; biçim KEK'ten
>    önce denetlenir; hata metni yalnız uzunluk. **Sayılan olgu:** bir `Wrap` ref'i bir `Seal`
>    zarfıdır (`TestSeal_WrapIsTheSameFormat`) — plaket anahtarını TOTP sırrından ayıran biçim
>    değil, KEK (OP-7'nin eşitsizlik reddi) ve AAD boyu (7 ≠ 16).
> 3. **TOTP:** yalnız stdlib; RFC 4226 Ek D (6 hane) ve RFC 6238 Ek B'nin **18 satırının 18'i**
>    (SHA-1/256/512, errata tohumları, T değerleri dahil) — yayımlanmış tablolar, bağımsız olarak
>    python'un `hmac`'iyle de yeniden üretildi. ±1 adım; pencere **bütün** yürünür (erken çıkış
>    yok, tek `subtle.ConstantTimeCompare` — `TestVerifyCode_TheWindowIsWalkedWholeInConstantTime`
>    kaynağı okur, sayıyı `constantTimeInventory` tutar); iki adım eşleşirse **sonraki** kabul
>    edilir (ikisi de emekliye ayrılsın diye — gerçek bir çakışma aranarak sınandı).
> 4. **Parola:** cost 12, ≥14 rune, ≤72 bayt, geçersiz UTF-8 ret, kompozisyon kuralı yok.
>    Sahte digest **literal değil, `New` anında** üretilir (maliyeti yapısal olarak `Cost`; depoya
>    digest biçimli dize girmez; bedeli süreç başına bir bcrypt). "Aynı yanıt, aynı süre":
>    bilinmeyen / yanlış parola / `pending` / `disabled` / 72+28 bayt / 300 baytlık adres —
>    hepsi aynı `ErrRefused`, **tam 1** bcrypt karşılaştırması (sayılarak), **tam 1** satır
>    (`TestPassword_EveryArmPaysOneComparisonAtTheSameCost`); maliyet eşitliği
>    `TestPassword_TheDigestAndTheDummyAreBothCostTwelve`.
> 5. **Oturum token'ı ve çerez:** 256 bit, `platform_sessions.token_hash` = küçük-hex
>    HMAC-SHA256(`TAPPA_OPERATOR_TOKEN_HMAC_KEY`, token dizgesi); yer tutucu
>    `operatorauth.SessionToken(redacted)` — panelinki ve çalışanınki **o tipler render edilerek**
>    karşılaştırılır (`TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder`).
>    `__Host-taptime_op` (8 sa Max-Age ipucu) ve ara çerez `__Host-taptime_op_login`: ikisi de
>    **daima** `Secure` — `__Host-` öneki Secure'suz çerezi tarayıcıya reddettirir, yani panelin
>    `insecure` gevşemesinin burada karşılığı yok; sonucu: https ve `http://localhost` dışında
>    düz http'de kimse giriş yapamaz (fail-closed). `Domain` yok, `Path=/`, `HttpOnly`,
>    `SameSite=Strict` (`TestCookies_AreHostPrefixedStrictAndSecure`, Set-Cookie METNİ üzerinde).
> 6. **Ara çerez (giriş challenge'ı) — ADR 0020 "Karar verilmedi"nin ömür ve anahtar maddesi:**
>    `v1 ‖ hesap id ‖ düzenlenme anı ‖ 16 bayt nonce` + HMAC-SHA256. **Ömür 5 dk + 1 dk geri
>    saat toleransı = 6 dk taşıyıcı pencere** (panelin `adminChoiceTTL` emsali; daha uzun ömür
>    daha çok tahmin vermez — tahminleri hesap bütçesi ve DB kilidi sınırlar). **Anahtar:**
>    `TAPPA_OPERATOR_TOKEN_HMAC_KEY`'den etiketle türetilir. ADR 0020 §2'nin yasağı **müşteri**
>    oturum anahtarından türetmeyedir (operatör kimliği müşteri anahtarından bağımsız olmalı);
>    bu türetme operatörün kendi anahtar ailesinde kalır, beşinci bir sır eklemez, ve ham
>    anahtar oturum hash'lerini ürettiği için türetilmiş anahtar bir challenge MAC'inin asla bir
>    oturum hash'i olamamasını sağlar. **Tek kullanımlık DEĞİL** (sayılı sınır: pencere içinde
>    yeniden sunulabilir; her sunuş hesap bütçesinden düşer). Base64 **katı** çözülür — ölçüldü:
>    gevşek çözücü son karakterin dolgu bitini çevirince 200 000 alanın 37 375'inde (41 bayt) ve
>    37 466'sında (32 bayt) AYNI baytları verdi (0,187 = 3/16, iki yazımlı kimlik bilgisi); katı
>    çözücü 0. *(2026-09-30 doğrulaması, yeniden ölçüldü, başka rastgele örnek: 37 792 / 37 447, katı
>    0. Oranın kaynağı: kanonik son karakterin dolgu bitleri sıfır olan 16 değerinden yalnız
>    `0`, `4`, `8`'in ASCII'sinde en düşük bitin çevrilmesi alfabede kalıp yalnız dolgu bitini
>    değiştiriyor — 3/16.)*
> 7. **Enrollment — ADR 0021 "Karar verilmedi"nin "düz sır nerede" maddesi: SUNUCUDA HİÇBİR
>    YERDE.** `BeginEnrollment` sırrı üretir ve bir **bekleyen blob**a mühürler (sayfanın gizli
>    form alanı); `CompleteEnrollment` açar. Sunucu düz sırrı yalnız iki isteğin **içinde** tutar;
>    bellek seçeneği seçilmediği için "kayıt sayısı ve ömrü tavanı" sorusu doğmaz (GET bir
>    `crypto/rand` okuması + bir `Seal` öder, istekten uzun yaşayan hiçbir şey ayırmaz). Blob'un
>    AAD'si `etiket ‖ id ‖ son geçerlilik` — saklanan zarfınki (yalnız id) DEĞİL, dolayısıyla biri
>    ötekinin yerine **iki yönde de** geçemez (`TestPendingBlob_IsNotTheStoredEnvelope`); ömür
>    30 dk (bağlantının TTL'i; DB token süresini ayrıca, duvar saatiyle uygular). Sır DB'ye ancak
>    ilk kod doğrulandıktan sonra, saklanan AAD ile yeniden mühürlenerek gider.
>    `EnrollmentTokenHash` **tek tanım** ve canlı sunucuya pinli (ASCII dışı dahil —
>    `TestEnrollmentTokenHash_IsTheDatabasesHash`). **OP-9'a, adıyla:** token
>    `operatorauth.NewEnrollmentToken` ile basılır (43 karakter base64url — `CompleteEnrollment`
>    başka biçimi `op_complete_enrollment`'a ve digest'e gitmeden reddeder, ~~DB'ye gitmeden~~
>    *(6. tur: tavanın altında bir `enrollment_failed` satırı yazılır)*) ve hash `EnrollmentToken.Hash()`/`EnrollmentTokenHash`
>    ile yazılır.
> 8. **Bütçeler (ADR'nin OP-6/OP-8'e bıraktığı sayılar; nüfus panelinki değil — 1–3 operatör):**
>    `flood` 300/10 dk adres başına (OP-8'in `floodGate`'i: `AllowRequest`) · `work` 20/10 dk
>    adres başına, bcrypt'e ulaşan HER istek (başarı dahil — `adminLoginWorkLimit` dersi) ·
>    `account` 10/10 dk **operatör başına, TOTP denemesi, kod denetlenmeden ÖNCE — ve bu, panelin
>    tersine, KAPIDIR**: challenge yalnız doğru parolayla basılır, yani bu bütçeyi yalnız parola
>    sahibi harcar; kapı olmasaydı kaydedilemeyen bir hata denetlenmiş bir tahmin olurdu ·
>    `auditCap` 30/10 dk **süreç geneli** (parolasız türler: `unknown_email`, `login_failed`,
>    `enrollment_failed`) — satır maliyeti ~~ÖLÇÜLDÜ (5 000 satır, geri alınan işlem): 81,9 B yığın,
>    **139,3 B** üç indeksle → en kötü sürekli durum 1 576 800 satır/yıl ≈ **219 MB/yıl**~~
>    **2026-09-30 doğrulamasında YENİDEN ÜRETİLEMEDİ, yeniden ölçüldü** (tablonun geçici kopyası —
>    aynı sütunlar, varsayılanlar ve üç indeks — geri alınan işlemde; gerçek tabloya dokunulmadı):
>    hedefsiz satır (`unknown_email`) yığın 85,2 B + indeks 81,9 B = **167,1 B** (5 000 satır) /
>    **158,6 B** (50 000 satır); hedefli satır (`login_failed`, `target_admin_id` dolu) **185,1 B** /
>    **174,7 B** → en kötü sürekli durum 1 576 800 satır/yıl ≈ **250–292 MB/yıl**. Eski "81,9 B
>    yığın" bu ölçümün **indeks** payına eşit — büyük olasılıkla etiket kayması; eski toplam
>    yeniden üretilemedi. *(2. tur, 2026-09-30: denetçinin bağımsız ölçümü — aynı geçici kopya,
>    `pg_total_relation_size` — hedefsiz 159,7–170,4 B, hedefli 176,3–188,4 B verdi. İki ölçüm
>    birlikte bir **gözlem aralığıdır, koşudan koşuya değişir**: hedefsiz **158,6–170,4 B**,
>    hedefli **174,7–188,4 B** → en kötü sürekli durum ≈ **250–297 MB/yıl**.)* Sayı `limits.go`
>    yorumunda ve ADR 0021'in notunda da bu bantla yazıldı; tavan kararı (30) değişmedi —
>    sınırlı bir büyüme, sınırsız değil. Tavan
>    aşılınca istek YİNE hizmet görür, yalnız satır yazılmaz (bir botnet'e operatör girişini
>    kapatan bir anahtar vermemek için) — bir **iz susturma ilkeli** olarak sayıldı, pencere başına
>    tek WARN (adres ve e-posta yok) · `enroll` 10/10 dk süreç geneli (ADR 0021 sınır 12'nin
>    süreç yarısı; ADR sayıları OP-8'e bırakıyor — öneri olarak burada, OP-8 aritmetiğiyle
>    değiştirebilir). *(2. tur, 2026-09-30 — `limits.go`'nun "sustained distributed flood"
>    cümlesi YANLIŞTI, ölçüldü: bu bütçeyi **tek bir adres** tüketir. `BeginEnrollment` veritabanı
>    okumaz, yani herkes rastgele bir id için sayfa açıp gösterilen sırrın geçerli ilk kodunu
>    yazabilir ve biçimi doğru rastgele bir token gönderebilir — Go'nun her kontrolü geçer, bütçe
>    harcanır, yalnız `op_complete_enrollment` reddeder. `work` (20/adres) `enroll`'dan (10/süreç)
>    büyük olduğu için tek adresten on istek pencerenin geri kalanında **her** adresten
>    enrollment'ı reddettirir; denetçinin sondası ve bu turun yeniden koşusu: tek adresten 10
>    denemeden sonra başka adresten meşru enrollment → `ErrThrottled`. ~~Hiçbir satır yazılmaz,
>    hiçbir hesaba dokunulmaz; gerçek operatör pencereyi bekler ya da `opadmin` yeni link verir.~~
>    **3. tur (2026-09-30), 2. denetçinin ölçümü, bu turda denetçinin sondası kendi kopyamda
>    yeniden koşuldu — aynı sonuç:** her saldırı isteği bir `enrollment_failed` satırı **yazar**
>    (pencere başına 10; süreç geneli audit tavanının 30'undan düşer); **yeni link kaçış değildir**
>    (yeni pending hesap + token, üçüncü adresten → `ErrThrottled` — bütçe süreç geneli); ve aynı
>    adres bir sonraki pencerede saldırıyı tekrarlar (`work` bütçesinin 10/20'si) → saldırı tek
>    adresten **süresiz** sürdürülebilir. ~~dakikada ~1 istekle … sayacı yalnız süreç yeniden
>    başlatması sıfırlar~~ — **4. tur düzeltmesi (3. denetçi):** sayaç sabit penceredir ve süre
>    dolunca gelen ilk istekte kendiliğinden sıfırlanır (`limits.go` `charge`); sürekli ret her
>    pencerenin başında **10 isteklik bir patlama** ister — ortalama ~1/dk olsa da eşit yayılmış
>    istekler bütçeyi pencerenin çoğunda açık bırakır; o pencerenin patlamasından önce gelen meşru
>    bir enrollment geçer. Sıfırlanma pencerenin kendisidir (yeniden başlatma da sıfırlar, ama
>    gerekmez). Hiçbir hesaba dokunulmaz.
>    **OP-8'e, adıyla:** sayılar orada; öneri (ölçülmedi, karar OP-8'in): `enroll`'un adres başına
>    payı `enrollLimit`'in altında — ör. adres başına 3/10 dk (bir operatörün birkaç denemesi
>    sığar) — tek adresin tüketmesini imkânsız kılar (en az dört adres gerekir); id başına pay işe
>    yaramaz, id'ler çağıranın kendisinindir.)* **12c (2026-10-01, güvenlik denetiminin ORTA
>    bulgusu, orkestratörün kararı — KAPANDI):** `enrollAddr` 3/10 dk **adres başına**, Go'nun her
>    kontrolünden sonra ve süreç geneli bütçeden ÖNCE (`work` → `enrollAddr` → `enroll` → bcrypt);
>    reddettiği istek ne satır yazar ne süreç sayacını ilerletir. Tek adres KENDİ penceresi başına
>    3 harcar; pencereler sabit ve her anahtarınki kendi ilk şarjıyla açılır, hizalı değildir
>    (12d, kapanış denetçisinin ölçümü, enjekte saat): pencere sınırında tek adres bir süreç
>    penceresine 5'e kadar koyabilir; tek bir süreç penceresi iki anahtar ve önceki bir istekle
>    tükenir; SÜREKLİ tüketim en az DÖRT hız anahtarı ister. Kalan sınır tek
>    listede (P8). Sayılar OP-8'in aritmetiğiyle değişebilir. Rakamlar `TestBudgets_TheShippedNumbersArePinned`'de literal. Kabul:
>    `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter` — beş bütçe de (12c'den) üretimin kurduğu
>    nesneyle, sınıra kadar harcanıp bir kez daha istenerek: satır 0, sayaç değişmez, bcrypt 0;
>    her birinde pozitif kontrol. *(2026-09-30 doğrulaması: "üretimin kurduğu nesne" = `build`,
>    `New`'un yürüdüğü kurucu yolu; testler `New`'u değil `build`'i çağırır, bütçeler aynı
>    `newLimits`'ten doğar. İki boşluk ölçülüp kapatıldı: **enrollment'ın** adres başına `work`
>    harcaması hiçbir testte değildi — silinince paket yeşil kaldı; aynı teste bir enrollment kolu
>    eklendi (bütçesi tükenmiş adresten bozuk token → `ErrThrottled`, satır 0). Ve `flood`
>    bütçesinin davranışı yoktu (`AllowRequest` hep "evet" deyince yeşil) →
>    `TestAllowRequest_RefusesPastTheFloodLimitPerAddress`.)* *(2. tur, 2026-09-30, denetçi
>    bulguları: audit tavanı testte yalnız `unknown_email` ile sürülüyordu — `login_failed` ve
>    `enrollment_failed`'ı tavanın etrafından doğrudan store'a yazan iki mutasyon YEŞİLDİ; aynı
>    test artık üç türün üçünü de önce tavanın altında (her biri kendi türünden 1 satır — pozitif
>    kontrol), sonra tavan tükenmişken (0 satır, aynı hata) sürer. Ve enrollment bütçesinin
>    bcrypt'ten ÖNCE harcandığı yalnız sonuçtan okunuyordu — sırayı ters çeviren mutasyon YEŞİLDİ;
>    `Authenticator`'a `compareFn` emsaliyle bir `digestFn` alanı eklendi (yalnız testler
>    değiştirir) ve test reddedilen istekte **0 digest, 0 veritabanı çağrısı** sayar; kontrol:
>    bütçenin altında, Go'nun her kontrolünü geçen yanlış token'lı istek 1 digest + 1 çağrı öder.)*
>    *(2. tur: saklanamayan adres — NUL baytı ya da geçersiz UTF-8 — "aynı yanıt, aynı süre"
>    kümesinin sayılmamış istisnasıydı, ölçüldü: parola adımı `database error (SQLSTATE 22021)`
>    döndürüyordu, 0 bcrypt, 0 satır. İki çare tartıldı: 22021'i `ErrNoOperator`'a eşlemek bir DB
>    turu daha öder ve aynı adresi taşıyan `unknown_email` satırı yine 22021 ile düşer (ölçüldü:
>    adresi satırla gönderen mutasyon `database error (SQLSTATE 22021)` verdi); seçilen:
>    `internal/db` böyle bir adresi aşırı uzun adresin yolundan geçirir — arama DB'ye gitmeden
>    `ErrNoOperator`, satırda adres NULL. Sonuç: `ErrRefused`, 1 bcrypt (sahte digest), 1
>    `unknown_email` satırı (hedefsiz) — `TestPassword_EveryArmPaysOneComparisonAtTheSameCost`'a
>    iki kol, `TestOperatorAccessors_AnUnstorableAddressIsNoAnswerNotAnError`. **OP-8'e, adıyla:**
>    form sınırında adres doğrulaması (UTF-8, NUL, uzunluk) bu kolu yeniden açmamalı — reddi aynı
>    `ErrRefused` yolundan vermeli.)*
> 9. 🔴 **İTİRAZ, ÖLÇÜMLE — ADR 0020 §3 / ADR 0021 §1 (a)'nın düz okuması:** *"oturum öncesi
>    satırlar … süreç genelinde bir tavanla sınırlanır"*. `totp_failed` ve `locked` bu tavanın
>    **dışındadır**: kilit sayacı yalnız `totp_failed` satırıyla ilerler (00026), yani parolasız
>    çöp tavanı doldurup `totp_failed`'ı susturabilseydi **kilidi kapatırdı** — OP-5'in DB
>    tarafında ölçtüğü aynı kusur (madde 10), bir katman yukarıda. Bu iki tür, yalnız parola
>    sahibinin harcayabildiği `account` kapısıyla × aktif operatör sayısıyla sınırlıdır: IP'den
>    bağımsız, ama tek bir sayı değil. `TestLimits_TheAuditCapCannotSwitchTheLockOff` pinler
>    (tavan tükenmişken 5 yanlış kod → 5 satır, sayaç 5, kilit); mutasyon ölçümü raporda.
>    *(2026-09-30 doğrulaması, itiraz yeniden kuruldu: yanlış kodun `totp_failed` satırını ortak
>    tavandan geçiren mutasyon testi kırmızıya çevirdi — mesajı birebir: "with the audit cap
>    exhausted: 0 totp_failed row(s), counter 0, want 5 and 5". Ama pin **yarımdı**: veritabanının
>    reddettiği, Go'nun kabul ettiği kodun satırını (tekrar edilen kod → `totp_failed`, kilitliyken
>    doğru kod → `locked`) tavandan geçiren mutasyon test **yeşil** kaldı — o kolun `totp_failed`'ı
>    da sayacı ilerletir, yani tekrarlar tavan dolunca bedava olurdu. Test iki yarıyla genişletildi
>    (tavan tükenmişken tekrar edilen kod → 1 `totp_failed` satırı, sayaç 1; kilitliyken doğru kod →
>    1 `locked` satırı); aynı mutasyon artık kırmızı. "Parola sahibi" kesin okuması: doğru parolayla
>    basılmış bir challenge'ı **tutan** — challenge taşıyıcı değerdir ve tek kullanımlık değil
>    (md. 6); tutan kişi hesap bütçesini harcayabilir, parolayı bilmesi gerekmez.)*
> 10. **Kilit sayıları KORUNDU: N = 5, pencere 15 dk** (00026'nın geçici sayıları; değişiklik =
>     migration, bu görevde yok). Aritmetik: ±1 pencere tahmin başına 3/10⁶; kilit şekli ilk
>     kilitten sonra pencere başına en çok 1 tahmin (günde 96 + ilk 5) → parolayı zaten bilen ve
>     bir yıl boyunca her 15 dakikada deneyen biri için ~35 000 tahmin ≈ **%10/yıl**, ve her tahmin
>     bir `totp_failed` satırı bırakır (OP-14'ün görüntüleyicisinde görünür). Daha sıkı bir şekil
>     (artan pencere) migration ister — sayılı sınır olarak açık, karar gerekirse orkestratörün.
>     Kabul: `TestLock_ThresholdAndWindowThroughTheSignIn` (4 → açık, 5 → kilit ~900 sn, kilitliyken
>     doğru kod DB'ce reddedilir ve `locked` satırı yazılır — sayılmaz; kilitliyken yanlış kod
>     sayılır ve uzatır; pencere geçince doğru kod açar, sayaç 0). *(5. tur, 2026-09-30: pencere
>     geçmiş, henüz başarı yokken — sayaç hâlâ eşiğin üstünde — DB'nin reddettiği doğru kod
>     `ErrCodeRejected` + bir `totp_failed` satırı alır ve sayar, `locked` DEĞİL. Bu vaka yokken
>     etiketi pencerenin bitişine bakmadan kuran mutasyon (L1) bütün pakette yeşildi: başarı yolu
>     etiketi okumaz.)*
> 11. **DB'nin reddettiği, Go'nun kabul ettiği kod** (tekrar, saat ayrışması, kilit) bir başarısız
>     deneme olarak yazılır — kilit okunmuşsa `locked`, değilse `totp_failed` (tekrarlanan bir kod
>     bedava olmasın). "Go kilit kararı vermez": okunan kilit damgası yalnız **etikettir**.
>     `TestTOTP_SameCodeFromNGoroutinesOpensExactlyOneSession` (8 yarışçı, ayrı havuz
>     bağlantıları, üretim şekli: 1 oturum, 1 `login`, 7 başarısız deneme satırı — `totp_failed`,
>     zamanlamaya göre birkaçı `locked` —, `-race`). *(4. tur: 7 ret
>     kilit eşiğinden (5) fazla olduğu için, hesabı beş ret commit edildikten SONRA okuyan bir
>     kaybeden `ErrLocked` + `locked` satırı alır — ilk sürüm bunu kabul etmiyordu ve ~125 paket
>     koşusunda 3 kez kırmızıydı; test artık zamanlamadan bağımsız olanı birebir ister: 1 kazanan,
>     her kaybeden için iki türden birinde 1 satır, `locked` yalnız ≥5 `totp_failed`'dan sonra.)*
>     *(5. tur: "yalnız ≥5'ten sonra" artık satırların YAZILIŞ SIRASIYLA ölçülür — `ORDER BY at,
>     id`, `at` `clock_timestamp()` — ilk `locked` satırından önce ≥5 `totp_failed`; 4. turun son
>     sayı biçiminde ilk reddi `locked` etiketleyen mutasyon (R3) yeşildi.)*
> 12. **`internal/httpx` import EDİLMEDİ — bütçe sayacı yerel bir kopya.** `httpx` panelin
>     kimliğini import ediyor (`adminidentity.go`) ve ADR 0020 §4'ün `requireOperator`'ının doğal
>     evi orası (`RequireAdmin` emsali): OP-8 onu oraya koyduğu gün `httpx → operatorauth` olur ve
>     tersi derlenmeyen bir döngüdür. Kopya `httpx.Limiter`'ın mekanizmasıdır (sabit pencere,
>     `maxKeys` tahliyesi) ve saati enjekte edilebilir.
> 13. **Zarf, sunucu hatasıdır, operatörün hatası değil:** başka satıra taşınmış zarf (AAD) ya da
>     başka KEK → hata, oturum yok, **satır yok, sayaç yok** (`TestTOTP_TheEnvelopeIsBoundToItsAccountAndItsKEK`,
>     kontrolüyle).
> 14. **Oturum kapısı:** `Verify` 8 sa / 30 dk / MFA'sız / iptal / `disabled` → `ErrNoSession`,
>     satır yok; sınırın bir dakika içi geçerli (`TestVerify_EveryDeadSessionIsRefused` — satırın
>     damgaları geriye yazılarak, yüklem DB'de duvar saatiyle).
> 15. **Kalıcı test verisi:** eşzamanlılık testi her koşuda dev DB'de 1 operatör hesabı, 1 oturum,
>     1 `login` ve 7 başarısız deneme satırı (`totp_failed`, zamanlamaya göre birkaçı `locked`)
>     bırakır (rastgele uuid, `@example.test`); diğerleri tek
>     geri alınan işlemde.
> 16. **Kapsam genişlemesi, gerekçeli: OP-5'in test yardımcısı `opTx`'e bir danışma kilidi.**
>     İlk tam koşu (`go test -race ./...`, `.env` yüklü) iki OP-5 testini kırmızı verdi:
>     `TestOperator00026_AppHoldsNothingUnderTheProductionDefaultACL` **40P01** (kilitlenme) ve
>     `TestOperator00026_AuditLogRefusesTheOwnerToo` **55P03** (10 sn `lock_timeout`). Sebep bu
>     görevdi: o testler operatör tablolarında işlem içinde DDL koşuyor (00026 Down/Up,
>     TRUNCATE, düşürülen CHECK) ve OP-6'ya kadar bu tablolara başka paket dokunmuyordu;
>     `go test ./...` iki test ikilisini **paralel** koşturur. Çare: `internal/db`'nin `opTx`'i
>     `pg_advisory_lock(hashtext('tappa/test/operator-tables'))`'u **özel**, operatorauth'un DB
>     testleri **paylaşımlı** alır (oturum düzeyi; bağlantı kapanınca bırakılır; her iki tarafta
>     da tablo kilitlerinden ÖNCE — döngüye giremez). İki yazımın eşitliğini
>     `TestHarness_TheTablesLockIsTheOneInternalDBTakes` kaynaktan okur. Sonrası: iki paket
>     birlikte yeşil; iki tam koşuda da `internal/db` yeşil (kalan tek kırmızı bilinen T72). ⚠️ **OP-7/OP-8'e, adıyla:** operatör tablolarına
>     dokunan her yeni DB testi aynı kilidi **paylaşımlı** almalı. *(2026-09-30 doğrulaması: bu
>     not yalnız burada, OP-6 bloğunda duruyordu; OP-7 ve OP-8'in kabul listeleri — yukarıda, OP-4
>     bloğunun "Kabullere bağlananlar" bölümü — onu taşımıyordu, yani OP-7'yi o listeden okuyan
>     yapıcı görmezdi. İki listeye de adıyla eklendi. Kilidi `internal/db`'nin tarafında özelden
>     paylaşımlıya çeviren mutasyon `TestHarness_TheTablesLockIsTheOneInternalDBTakes`'i kırmızıya
>     çevirdi. 2026-09-30 tam koşusunda iki paket birlikte yeşil.)*
> 17. **Test bedeli, ölçüldü (bu makine, `-race`, 2026-09-26):** bir cost-12 bcrypt ~4,4 sn. İlk
>     sürümde her test kendi `New`'unu çağırıyordu (her biri bir sahte digest bcrypt'i) ve tam
>     koşuda paket **380 sn** sürdü; `internal/db` paylaşımlı kilidi beklerken 101 → **331 sn**'ye
>     uzadı. Düzeltme: testler tek bir sahte digest'i `build` üzerinden paylaşır (üretim yolu —
>     `New`'un kendi digest'i — `TestPassword_TheDigestAndTheDummyAreBothCostTwelve` ve harici
>     testlerce sürülür), konusu parola olmayan TOTP/oturum testleri challenge'ı doğrudan basar,
>     ortak digest'ler kilit alınmadan önce ısıtılır. Sonra tam koşu: paket **240 sn**,
>     `internal/db` **62,9 sn**, duvar saati **480 sn** (en uzun paket `internal/handler` 473 sn —
>     bu paket kritik yolda değil). Kalan bedel testlerin KONUSU olan karşılaştırmalardır.
>     *(2026-09-30 doğrulaması: bu maddenin sayıları **önceki yapıcının tek koşusudur**; "~4,4 sn",
>     "380 sn" ve "101 → 331 sn" artık var olmayan bir sürüme aittir, **yeniden ölçülmedi**.
>     **Yüke bağlı gözlem aralığı, nokta değil (M6-04 dersi):** yedi tam koşu (`.env` yüklü,
>     `-race -count=1 ./...`, aynı makine, iki yürütücü — bu doğrulamanın beş koşusu ve 2.
>     denetçinin iki koşusu, 2026-09-30): paket **110,9–180,7 sn**, `internal/db` **41–120,8 sn**,
>     duvar **281–348 sn**. Paketin üst ucu 3. turun sızıntı testinden gelir: süreç geneli
>     enrollment bütçesini tüketmek (E7 kolu) sekiz cost-12 digest öder, `-race` altında testin
>     kendisi ~34 sn. Dolaylı bcrypt
>     okuması: `TestPassword_TheDigestAndTheDummyAreBothCostTwelve` (≥2 cost-12 üretim + 2
>     karşılaştırma) `-race` altında 9,98 sn.)*
>     **API notu (OP-8'e):** `TOTP(ctx, challenge, code)` adres ALMAZ — adres bütçesi floodGate'in
>     (`AllowRequest`), geçerli bir challenge ise doğru parolayla basılmıştır; anlamlı bütçe
>     hesabınkidir. `Password(ctx, addr, email, parola)` ve `CompleteEnrollment(ctx, addr, …)`
>     adresi `work` bütçesi için alır ve hiçbir satıra yazmaz.
> 18. **Sayılı sınırlar (kapatılmadı, adıyla).** *(11. tur: geçerli olan bu maddenin sonundaki
>     **tek liste**dir; bu paragraf tarihçedir — neyin ne zaman eklendiği ve kapandığı.)*
>     Challenge tek kullanımlık değil (md. 6) · audit
>     tavanı bir iz susturma ilkelidir (md. 8) · bütçeler süreç içidir (yeniden başlatma sıfırlar,
>     iki replika her tavanı ikiye katlar) · `Secret.Base32`/`URI` düz metin dize döndürür (Go
>     dizgesi silinemez; kalıntı kayıt ekranınındır) · TOTP kodu ve parola, handler'dan düz `string`
>     olarak gelir (sızıntı testi numaralı kolların hatalarını ve Debug log'u numaralı render'larda tarar — 5. tur sözleşmesi —, tip duvarı değildir) · giriş
>     aramasını definer'a taşıma önerisi (ADR 0021 sınır 11) **benimsenmedi** — yeni definer ve rol
>     migration ister; `tappa_operator` digest'i ve zarfı okumaya devam eder. *(2026-09-30
>     doğrulamasının eklediği, analizle, ölçülmedi:)* kilitliyken doğru kod ile yanlış kod **aynı
>     hatayı** (`ErrLocked`) alır ama doğru kod bir veritabanı çağrısı fazla öder
>     (`op_open_session` reddi + `locked` satırı; yanlış kod yalnız `totp_failed` satırı) — bir
>     zamanlama farkı. Pencere başına etkili tahmin sayısını **artırmaz**: kilitliyken bulunan bir
>     kod ancak kilit bitince kullanılabilir, kilit son YANLIŞ tahminden 15 dk sonra biter ve kodun
>     ömrü ±1 adımdır (~60–90 sn) — yani işe yarayan bir tahminin önünde ~14 dk yanlış tahminsiz bir
>     aralık olmak zorunda; md. 10'un "pencere başına en çok bir tahmin" aritmetiği korunur.
>     *(6. tur, 2026-09-30, adıyla ve ölçülerek:)* ~~çağıranın dışa kapalı alanında tutulan bir
>     `Config` DEĞERİ anahtarlarını yansımayla basar (…`knownLeaks`… OP-7'ye kural olarak
>     devredildi)~~ — **7. turda kapatıldı:** anahtarlar `Key` (`*string`); o sınır ve
>     `knownLeaks` yok · sızıntı testinin hasat pini sayı ve arite tutar, içerik değil (doğru biçimde yanlış değer
>     kaydeden sahte store görülmez) · audit-satırı AST okuması ada göredir (başka adla satır
>     yazan yeni bir `Store` metodu ve reflection görülmez; değişken tür yalnız kendi
>     fonksiyonunun ya da paket düzeyi bildiriminin atamalarından çözülür, çözülemeyen "?"
>     olarak işaretlenir) · ~~kapalı tip kümesinin `internal/db` yürüyüşü dışa açık alanları ve
>     yöntem imzalarını izler~~ *(7. tur: tip denetimi — dışa açık fonksiyon, değişken, sabit ve
>     yöntem imzaları dahil)*; kapalı küme ÇALIŞMA ZAMANINDA başka bir paketin tipiyle doldurulan
>     bir `any`'yi görmez ~~(küme bu paketin ve `internal/db`'nin adlı tipleridir) · alan kontrolü
>     map/slice ELEMAN tiplerine inmez~~ *(11. turda ikisi de değişti: kayıt modülün her
>     paketinin adlı tipleri; kural (2) dilim/dizi elemanına ve map değerine özyinelemeli —
>     tek liste, S1 ve S3–S4)*. *(7. tur, eklenen:)* `Secret.Zero` baytları SİLEMEZ
>     (Go dizgesi değişmez), değeri unutturur — ekranın `Base32`/`URI` dizgeleri zaten
>     silinmiyordu; oturum açılışında ve enrollment'ta açılan sır yerel `[]byte`'tır ve
>     `sun.Zero` ile silinir. `Key.bytes()` her kullanımda bir kopya üretir, silinmez — anahtar
>     süreç boyunca zaten bellekte (`internal/config`'in her anahtar için sahip olduğu
>     emanet). *(8. tur, eklenen, ölçülerek:)* ~~kapalı tip kümesi, başka bir paketin
>     BİLDİRDİĞİ bir tipin içinden … ulaşılan tipi izlemez (öteki paketler yüklenmez …)~~ →
>     **9. turda değişti:** tipler artık KESİN yüklenir (derleyicinin export verisi);
>     **10. turdan:** yürüyüş, go/types'ın tip grafiğini tükenmiş bir anahtarla tam dolaşır
>     (her tür adıyla; tanınmayan tür = kırmızı; her paketin tipi, her struct alanı dışa açık
>     ya da kapalı) — 9. turun "başka paket yalnız tip argümanıyla" öncülü yanlıştı ve
>     kalktı; kapalı kümenin kalan sınırı yalnız ÇALIŞMA ZAMANINDA doldurulan bir `any` ve
>     reflection · arayüz tipli bir alanın ÇALIŞMA ZAMANI içeriği (`Authenticator.store`'a konan
>     bir `[]byte` ya da düz DSN'li bir yapı) alan kurallarının dışındadır · *(9. turda ölçüme
>     göre yeniden yazıldı:)* `store`'un DÜZ bir alanı, `Authenticator`'ın değer ya da işaretçi
>     olarak tutulmasından bağımsız olarak basılabilir (8. denetçinin 28 fiillik ölçümü: düz
>     alanlı değer store'u her fiilde, düz alanlı işaretçi store'u 14 fiilde, dışa kapalı
>     alandaki `*Authenticator` 14 fiilde; `*string` alanlı store hiçbirinde) — **kural: düz
>     alan yok** (OP-7 devri md. 4) · ~~ve bütçe haritalarının anahtarlarını (istemci
>     adresleri, operatör id'leri) basar — `badVerb` `*budget`'i bir kez açar;
>     ~~`limits *limits` yolu kapatır …, ürün değişikliği kararı sonraya bırakıldı~~ → **8b,
>     KAPANDI (orkestratörün kararı):** `Authenticator.limits` artık `*limits`; pinli (sızıntı
>     matrisinin `Authenticator` specimen'inin bütçe haritası dolu, adres sırlarından biri;
>     `limits limits` geri dönüşü kırmızı). `Authenticator`'ın öteki değer/işaretçi alanları
>     sayıldı: `store` (arayüz — içeriği OP-7'nin, kural OP-7 devrinde), `keys` (`authKeys`
>     değeri — alanları `Key`, matriste ölçüldü), `log` (`*slog.Logger` — `badVerb` bir kez açar,
>     yalnız handler'ının ADRESİNİ basar; adres ya da kimlik bilgisi tutmaz), `now`/`compareFn`/
>     `digestFn` (fonksiyon — adres basılır); `store`'un kuralı OP-7 devrinde.~~ *(9. tur: bütçe
>     yarısı 8b'den beri kapalı — 8. denetçi 0 isabet ölçtü; sayım cümlesi 8. tur alt
>     bölümünde.)*
>
>     **Tek liste (11. tur, 2026-10-01; 12. turda güncellendi: S3, S4, S6, S7 yeniden yazıldı,
>     S11–S14 eklendi).** OP-6'nın sayılı sınırları YALNIZ burada tutulur; test yorumları
>     (`leak_external_test.go`: kapalı küme testi, `notSpecimens`, `isBytesOrText`,
>     `redactedValues`, `printed`, `exactImports`, sızıntı testinin NOT CLAIMED listesi;
>     `units_test.go`: AST okuması ve sahte digest), ADR 0020'nin OP-6 notu ve OP-8 devri buraya
>     işaret eder. Neden: 10. turda kopyalar ayrıştı — üçüncü bir sınır (S2) yalnız test
>     yorumunda ve 10. tur bloğundaydı. Kural (1)'in (her alan adıyla ve tipiyle) 12. turdan beri
>     muafiyeti YOK: fonksiyon ve `okFieldTypes` tipli alanlar da adlıdır.
>     *Ürün:*
>     - **P1** — challenge tek kullanımlık değil (md. 6).
>     - **P2** — audit tavanı bir iz susturma ilkelidir (md. 8).
>     - **P3** — bütçeler süreç içidir: yeniden başlatma sıfırlar, iki replika her tavanı ikiye katlar.
>     - **P4** — giriş aramasını definer'a taşıma önerisi (ADR 0021 sınır 11) benimsenmedi;
>       `tappa_operator` digest'i ve zarfı okumaya devam eder.
>     - **P5** — kilitliyken doğru kod yanlış kodla aynı hatayı alır, bir veritabanı çağrısı
>       fazla öder (zamanlama farkı; pencere başına etkili tahmin sayısını artırmaz — yukarıda).
>     - **P6** — Go dizgeleri ve kopyalar: `Secret.Zero` baytları SİLEMEZ, değeri unutturur;
>       `Secret.Base32`/`URI` dizgeleri silinmez (kalıntı kayıt ekranınındır); `Key.bytes()` her
>       kullanımda silinmeyen bir kopya üretir (anahtar süreç boyunca zaten bellekte).
>     - **P7** — TOTP kodu ve parola handler'dan düz `string` gelir; sızıntı testi numaralı
>       kolların hatalarını ve Debug log'u numaralı render'larda tarar, tip duvarı değildir
>       (OP-8'e redacting-tip önerisi, OP-8 devri).
>     - **P8** — (12c; 12d'de pencere metni düzeltildi) süreç geneli `enroll` bütçesini DAĞITIK
>       bir saldırgan yine tüketir. Adres payı (`enrollAddr`) anahtarın KENDİ penceresi başına 3;
>       pencereler hizalı değil — pencere sınırında tek anahtar bir süreç penceresine 5'e kadar
>       koyar, tek bir süreç penceresi iki anahtar ve önceki bir istekle tükenir; SÜREKLİ
>       tüketim en az dört hız anahtarı ister — dört IPv4 adresi ya da dört IPv6 /64'ü (bir /48
>       sahibi 65 536 tutar). Çare OP-8'in (K4 ops IP kısıtı ya da başka).
>     - **P9** — (12c) doğru parolası girilip TOTP'si tamamlanmayan giriş KALICI bir iz
>       bırakmaz: yalnız bir slog Info satırı (id'yle; süreç log'unun saklama süresi kadar);
>       `password_ok` audit türü bir migration'dır — OP-8/OP-14.
>
>     *Kapalı tip kümesi ve alan kuralı:*
>     - **S1** — ÇALIŞMA ZAMANI: kapalı küme, bir fonksiyonun doldurduğu `any`'yi (ya da her
>       arayüzü) ve yansımayla yapılan değeri görmez — küme tiplerin söylediğidir. Arayüz tipli bir
>       alan (`Authenticator.store`) bildirilen tipiyle yargılanır; içine konan `[]byte` ya da düz
>       DSN'li yapı alan kurallarının dışındadır (`store`'un kuralı OP-7 devrinde: düz alan yok).
>     - **S2** — bir adlı tipin DIŞA KAPALI yöntemleri yürünmez (paket dışından çağrılamaz,
>       yazdırılacak değer tutmaz).
>     - **S3** — İŞARETÇİ MODELİ (12. turda yeniden yazıldı; ~~"fmt bir kabın içindeki
>       işaretçiyi her fiilde adres olarak basar"~~ yanlıştı — 11. denetçi: `[]*[]byte`'taki
>       anahtar 269 render'ın 24'ünde basıldı). Ölçülen gerçek: fmt bir YOLDA BİR işaretçiyi
>       açar — alanın, elemanın, map anahtarı ya da değerinin, her derinlikte — dizi, dilim,
>       struct ya da map'i gösteriyorsa ve fiil bir işaretçinin almadığı bir fiilse (`badVerb`
>       `%v` ile yeniden basar; ondan sonraki her işaretçi adrestir); `encoding/json`
>       (`json.Marshal`, slog'un JSON handler'ı) DIŞA AÇIK bir alanın HER işaretçisini izler.
>       Kural (2) bu modeli okur. **Okumadıkları — yalnız dışa KAPALI bir alanda:** başka bir
>       şeyi gösteren işaretçi (`*string`, `**[]byte`), yoldaki ilk işaretçinin arkasındaki her
>       işaretçi (`[]*[]*[]byte`), kanal ve fonksiyon. Pin: `TestExportedTypes_ExemptFormsPrintNoKeyBytes`
>       bu biçimleri KEK'le doldurup matriste basar — muaf on biçim 0 yolda; yanlarındaki
>       okunan dokuz biçim (`[]*[]byte`, `map[string]*[]byte`, `[]*[32]byte`, dışa açık
>       `*string` …) 3–262 yolda — ve `isBytesOrText`'in cevabını ölçümle eşler.
>     - **S4** — kural (2)'nin OKUMADIKLARI: `string` TÜRÜNDE bir map anahtarı (`budget.windows`;
>       anahtarlarını `checkBudgetKeys` İÇERİKLE pinler — 12b: flood/work sürüşün kullandığı
>       istemci adreslerinden biri, account sürüşün operatör id'lerinden biri, auditCap/enroll
>       yalnız boş anahtar; ~~12. turun biçim pini (adres gibi ayrışır, uuid gibi ayrışır)~~
>       12. denetçinin beş mutantını yeşil bırakıyordu. **Sınır, adıyla:** yalnız SÜRÜLEN
>       yollar — 43 kol (12c: E10) ile `Verify` ve `Logout`'un başarı yolları; sürülmeyen bir yolun
>       şarjı görülmez) — başka türde bir anahtar okunur (`map[[32]byte]bool` kırmızı); tek bir
>       tamsayı (sayaç, `int` olarak kod); bool, float ya da karmaşık sayı dizisi;
>       `okFieldTypes` tipindeki değer (uuid bir bayt dizisidir, sır değildir — alanı yine de
>       kural (1)'de adlı); arayüzün çalışma zamanı içeriği (S1).
>     - **S5** — DÜZ DÖNÜŞ (Mk): dışa açık bir imzanın düz metin, bayt ya da modül DIŞI bir yapı
>       DÖNDÜRMESİ hiçbir kuralın konusu değildir — `func Snap() struct{ K []byte }` düz `[]byte`
>       döndürmekle eşdeğerdir (kayıt yalnız modülün adlı tiplerini tutar; alan kuralı kümenin ve
>       tuttuğu yapıların alanlarını okur). `Secret.Base32`/`URI` ve
>       `EnrollmentToken.RevealForLink` meşru olarak düz `string` döndürür: değerin çağırana
>       geçtiği yer bilinçli bir API kararıdır, gözden geçirmede okunur. Dışa açık PAKET
>       DEĞİŞKENİ ise 12b'den beri pinli: yalnız `error` olabilir (bugün 8 sentinel); başka
>       tipte bir değişken (`var LastKEK []byte`) kırmızı. 12d'den: her dışa açık `error`
>       değişkeni `errors.New(<dize literali>)` ile bildirilir ve paketin hiçbir yerinde yeniden
>       yazılmaz ya da adresi alınmaz (AST; bugün 8'i de bu biçimde); `New`'da KEK metniyle
>       doldurulan `var ErrLastKey error` kırmızı. Görülmeyen, adıyla: yansıma ya da `unsafe`
>       ile yazma.
>
>     *Specimen araması ve sızıntı testi:*
>     - **S6** — `dummyDigest` ARANMAZ: `Authenticator` specimen'inin tuttuğu beş `Key`'den
>       dördü aranır (12c'den: `pendingKey` dahil); `dummyDigest` `unsearched`'te adıyla — `New`'un çekip attığı 32 rastgele
>       baytın cost-12 bcrypt digest'i. 12. turdan pinli olan kısmı:
>       `TestDummyDigest_IsNotTheDigestOfAKnownValue` — sıfır tohumun (dolmayan bir okumanın
>       bıraktığı) ve boş parolanın digest'i DEĞİL (`rand.Read(nil)` kırmızı). Pinli OLMAYAN:
>       sabit, sıfır olmayan bir tohum (ölçüldü: yeşil); iki sahte digest'i karşılaştırmak hiçbir
>       şey kanıtlamaz (bcrypt her birini tuzlar).
>     - **S7** — aramanın pini (`TestSpecimens_SearchEveryRedactedValueTheyHold`) specimen'in
>       TUTTUĞU redakte değerleri okur; bir yaprağın oturabileceği ama DEĞERSİZ bırakılmış her
>       yer kırmızı (12. turdan: yaprak tutabilen bir tipe nil işaretçi, boş dilim ve boş map
>       dahil — `*struct{ K Key }`, `**Key`, `*[1]Key`, `[]Key{}`); `unsearched` girdileri yol
>       listesiyle pinli. Yürünmeyen: bu modülün DIŞINDAKİ bir paketin yapısının içi
>       (`atomic.Pointer`, `sync.Map`) ve nil bir arayüz (tipi, yani ne tutabileceği,
>       bilinmez); düz değerler (bütçe anahtarındaki adres) elle listelenir.
>     - **S8** — arama biçimleri: bölünmüş ya da kısmi değerler; listede olmayan render'lar
>       (ölçülüp bulunamayanlar adıyla); bu pakette olmayan asla-loglanmaz maddeleri;
>       dışarıdan başarısız kılınamayan kollar — sızıntı testinin NOT CLAIMED listesi.
>     - **S9** — hasat pini sahte store'un kaydettiklerinin SAYISINI ve aritesini tutar,
>       içeriğini değil (doğru biçimde yanlış değer kaydeden sahte store görülmez).
>     - **S10** — audit-satırı AST okuması ADA göredir: başka adla satır yazan yeni bir `Store`
>       metodu ve reflection görülmez; değişken tür yalnız kendi fonksiyonunun ya da paket
>       düzeyi bildiriminin atamalarından çözülür, çözülemeyen "?" olarak işaretlenir.
>     - **S11** — GEREKÇE METİNLERİ gözden geçirenin İDDİASIDIR: `allowedFields`,
>       `notSpecimens`, iç istisnalar ve `unsearched` girdilerindeki `why`. Testler her
>       girdinin ADINI, TİPİNİ ve KÜMESİNİ pinler (kural (1) istisnasız; kapalı küme iki yönlü;
>       `unsearched` ve — 12b'den — `okFieldTypes` literal listeyle), metnini ASLA. Bir gerekçeyi yanlışlayıp ad, tip ya da
>       küme değiştirmeyen bir mutant tanım gereği sözleşmenin dışındadır (ölçüldü: yanlış
>       yazılmış `dummyDigest` gerekçesi yeşil); üçünden birini değiştiren her mutant
>       kırmızıdır. Gerekçeler yine de ölçülen gerçekle yazılır.
>     - **S12** — `go list` hatası testin mesajına stderr'iyle girer: cmd/go proxy URL'sini
>       `url.URL.Redacted` ile basar (parola maskelenir); URL'nin KULLANICI ADI kısmına konmuş
>       bir kimlik bilgisi maskelenmez.
>     - **S13** — pozitif kontrol (`TestLeak_TheBareValueIsThePositiveControl`) yalnız-adres
>       yollarında (`%p` ile bir işaretçi, dilim ya da map tutucusu) bir şey gösteremez; arama
>       orada da koşar.
>     - **S14** — tip pini adlı bir tipi ADIYLA (tam paket yolu) tutar: adlı, struct ya da
>       arayüz olmayan bir tipin TANIMI aynı adla değişirse pin bunu görmez — kural (2) tanımı
>       yapısal okur (kural (2)'nin okuduğu bir biçime — dize, tamsayı dizisi, açılan bir işaretçinin
>       arkasındaki bayt — dönen her tanım kırmızı).
>       Bugün yürünen alanlarda böyle tipler yalnız modül dışıdır (`uuid.UUID`,
>       `time.Duration`); `Store` arayüzünün yöntemleri kapalı kümenin yürüyüşündedir.
>
> **Kabullere bağlananlar — OP-6, karşılıkları:** RFC 6238 Ek B → `TestTOTP_RFC6238AppendixB`
> (+ `TestHOTP_RFC4226AppendixD`) · aynı kod N goroutine → md. 11 · yanlış KEK açamaz →
> `TestOpen_AWrongKEKCannotOpen` + md. 13 · sızıntı testleri harici pakette →
> `internal/operatorauth/leak_external_test.go` (`TestLeak_NoSecretOnAnyPrintingPath`, pozitif
> kontrol `TestLeak_TheBareValueIsThePositiveControl`, `TestLeak_NoInputInAnyErrorOrLogLine`) ·
> 8 sa / 30 dk / MFA'sız / iptal / `disabled` → md. 14 · kilit eşiği ve penceresi → md. 10 ·
> zarf → md. 2 · düz sırrın yeri → md. 7 · limiter testi → md. 8. ~~**Mutasyonla:** 31 mutasyon
> (…) — **31'i de kırmızı**; birinin (±1'in genişletilmesi) ilk koşuda YEŞİL kaldığı ölçüldü:
> pencere testi beklentisini sabitin kendisinden hesaplıyordu (M6-01 B'nin totoloji sınıfı),
> test ADR'nin literal ±1'ine ve sabitlerin literal pinine çevrildi, aynı mutasyon kırmızı.
> Kapsam: `internal/sun` %97,4, `internal/operatorauth` %90,7.~~ *(Önceki yapıcının iddiası;
> raporu yoktu, mutasyon listesi ve çıktısı bulunamadı — aşağıdaki doğrulama bloğunda yeniden
> ölçüldü ve yerine geçti.)*
>
> **Kart düzeltmesi (2026-09-30, OP-6 doğrulama turu).** Önceki yapıcı haftalık kullanım
> limitiyle yarıda durdu; raporsuz, denetimsiz iş baştan sınandı. Ölçüm: dev Postgres 17.10, `.env`
> yüklü; mutasyonlar kopyala-geri-yaz (her birinden sonra dosyanın sha256'sı özgün olana döndü,
> 68/68), hedefli `-run`, `-race`'siz; tam koşu ve kapsam ayrıca. Düzeltilen iddialar madde
> madde yukarıda, *(2026-09-30 doğrulaması)* etiketiyle (md. 6, 8, 9, 16, 17, 18). Kalanlar:
>
> 1. **Mutasyon — 68 mutasyon; ilk ölçümde 58 kırmızı, 9 YEŞİL ve 1 derlenmeyen (MAC kontrolünü
>    kapatan biçim `mac`'i kullanılmaz bıraktı — derlenir biçimde yeniden kuruldu, kırmızı); 9
>    yeşilin her biri bir test güçlendirmesiyle kırmızıya döndü → son koşu 68/68 kırmızı.** Sınıflar: TOTP penceresi (daraltma, döngüde ve sabitte genişletme), erken
>    çıkış, sabit zaman (üç biçim), iki adım eşleşince sonraki, bayt sırası, kesme maskesi, epoch
>    öncesi taban · tekrar/kilit kolunun kaydı, dört bütçenin her biri (silme ve sıra), audit
>    tavanı, itiraz mutasyonu (md. 9) ve kardeşi, sahte digest ve maliyeti, zarf hatasının
>    sayılması, enrollment AAD'si / adımı / parola kuralı / red satırı, Go'nun DB reddini ezmesi ·
>    yer tutucu, `Format` sızıntısı, anahtarsız hash · token hash sözleşmesi · blob AAD'si / süresi /
>    süresinin AAD dışında kalması · challenge TTL sınırı / MAC / katı base64 / anahtar türetmesi /
>    gelecek saat · çerez `SameSite` / `Secure` / ad · bütçe tahliyesi, sınırsız harita, sayı pini,
>    `AllowRequest`, pencere yenilenmesi · kuşak (e-posta ve id), `PgError`, gömülü değer, 28000
>    eşlemesi, `operator.sql` ayna kayması · `Seal` sabit nonce / AAD'siz / yarım anahtar programı /
>    biçim sırası / boş AAD · danışma kilidinin paylaşımlıya dönmesi. **İlk koşuda YEŞİL kalan 9 ve
>    kapatan güçlendirme:**
>    - `want == in && subtle.ConstantTimeCompare(…) == 1` ve `_ = subtle.ConstantTimeCompare(…);
>      hit := want == in` — döngüde yine tek sabit-zaman çağrısı vardı →
>      `TestVerifyCode_TheWindowIsWalkedWholeInConstantTime` artık döngüdeki her `==`/`!=`'in bir
>      işleneninin o çağrı olmasını ister (iki mutasyon).
>    - DB'nin reddettiği kodun satırını ortak tavandan geçirmek → md. 9'un ek yarısı.
>    - enrollment'ın parola kuralını silmek (`hashPassword` kuralı yeniden uyguladığı için hata
>      aynıydı) → `TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens` 13 runelik vakada süreç
>      geneli enrollment bütçesinin harcanmadığını da ölçer.
>    - challenge MAC anahtarını `build`'de ham oturum anahtarı yapmak (fonksiyon pinliydi,
>      kullanımı değil) → `TestChallenge_KeyIsDerivedNotTheSessionKey` kurucunun ürettiği
>      Authenticator'da ham anahtarla imzalı challenge'ı reddettirir, türetilmişle kabul ettirir.
>    - `AllowRequest` hep "evet" → `TestAllowRequest_RefusesPastTheFloodLimitPerAddress`.
>    - id aramasından kuşağı (`status = 'active'`) silmek — tek sonda `pending` hesaptı ve
>      kimlik bilgisi olmadığı için `scanOperator` onu zaten "yok" sayıyordu →
>      `TestOperatorByEmail_TheBeltHoldsWhereRLSDoesNot` sahip olarak `disabled` hesabı da id ile
>      arar (kontrol: `active` bulunur).
>    - enrollment'ın adres başına `work` harcamasını silmek → md. 8'in ek kolu.
>    - challenge'ın hesabı artık `active` değilken koda devam etmek → yeni
>      `TestTOTP_AChallengeDoesNotOutliveTheAccount` (parola adımından sonra `disabled` edilen hesap:
>      `ErrRefused`, oturum/satır/sayaç yok; kontrol: yeniden `active` → aynı challenge ve kod
>      oturum açar).
> 2. **Kapsam — bu kartta TEK kaynak (12. tur sonunda yeniden ölçüldü, 8.–11. turla aynı;
>    `.env` yüklü, `-count=1`, `-race`'siz):**
>    `internal/sun` **%97,4**, `internal/operatorauth` **%95,6** (2.–5. turda %95,2, 6. turda
>    %95,3, 7. turda %95,5). Kapsanmayan dallar yalnız
>    `crypto/rand` okuma hataları ile `Seal`/bcrypt/token üretim hataları (hata enjeksiyonu
>    olmadan ulaşılamaz) — hiçbiri bir kabul kolu değil. Test sayısı (`-race -v`, üst düzey):
>    `operatorauth` 47 (4. turda `TestAuditRows_EveryPasswordlessKindGoesThroughTheCap`, 6. turda
>    `TestExportedTypes_EveryOneIsASpecimenOrANamedException`, 8. turda `TestKey_CopiesInAndOut`,
>    11. turda `TestSpecimens_SearchEveryRedactedValueTheyHold`, 12. turda
>    `TestExportedTypes_ExemptFormsPrintNoKeyBytes` ve `TestDummyDigest_IsNotTheDigestOfAKnownValue` eklendi; 5. ve 7. turda yeni test
>    fonksiyonu yok — vakalar mevcut testlere eklendi),
>    `sun` 176, SKIP 0.
> 3. **RFC vektörleri bağımsız yeniden üretildi** (python `hmac`): RFC 4226 Ek D'nin 10 satırı ve
>    RFC 6238 Ek B'nin 18 satırı (T değerleri dahil) testteki tabloyla birebir.
> 4. **Kapı zinciri (2026-09-30; 5.–12. tur sonunda baştan yeniden koşuldu, aynı sonuç —
>    6. turdan beri `gofmt -s -l` ile, `make fmt`'in biçimiyle; 7. turda staticcheck son kod
>    değişikliğinden SONRA):** `gofmt -l .` boş · `go build ./...` · `go vet ./...` ·
>    `make gen` sonrası `git diff --stat` aynı (sqlc `operator.sql`'den dosya üretmiyor —
>    `resolve.sql` gibi; `internal/store`'da fark yok) · `go.mod`/`go.sum`/`sqlc.yaml` diff boş ·
>    `./scripts/redline-check.sh` exit 0 · `TestEveryNamedTestExists` yeşil · `staticcheck` bu
>    makinenin Go 1.27.1'iyle T72'de çöküyor, önbellekteki `go1.26.7` araç zinciriyle temiz.
> 5. **Dev DB:** mutasyon ve tam koşular sonrası `pg_db_role_setting`'te operatör rolleri için 0
>    satır; `tappa_opdefiner`'ın dört operatör tablosu dışında tablo/sütun yetkisi 0; beş `op_*`'ın
>    sahibi `tappa_opdefiner`; rol üyeliği 0; `zz_*` nesnesi 0. Kalıcı test verisi (tasarım gereği,
>    temizlenmedi): eşzamanlılık testlerinin — bu paketin ve OP-5'in — her paket koşusunda
>    bıraktıkları; hesapların hepsi `@example.test`; audit türleri `enrollment`, `login`,
>    `totp_failed`, `locked` (sonuncusu hem yarış testinin kilitten sonra okuyan kaybedenlerinden
>    hem de DB reddini `locked` diye etiketleyen mutasyonların commit ettirdiği satırlardan).
>    ~~operatör hesap/oturum/audit satırı bu turun başında 35/39/161, … 4. turun sonunda
>    480/469/3371 … ve 14 `locked`~~ — **5. tur:** satır sayıları kaldırıldı. Her paket koşusuyla
>    artan bir sayı bir gerçek değil, bir anın ölçümüdür (M6-05 A dersi), ve "14 `locked`" zaten
>    yanlıştı (4. denetçi 31 ölçtü). Kayan sayı artık yazılmıyor.
>
> **2. tur (2026-09-30) — üçüncü göz RED: 4 bloklayıcı + 9 bloklamayan, hepsi kapatıldı.** Ürün
> kodu doğru bulundu; bulgular pinsiz korumalardı. Denetçinin 25 mutasyonu (`X*`, `Y*`, `Z1`, üç
> kontrol) aynı adlarla yeniden koşuldu — **25/25 kırmızı** (`Y20`'nin metni `digestFn` yüzünden
> uyarlandı, anlamı aynı); bu kartın kendi listesi 68 → **70** (saklanamayan adresin iki yarısı) —
> **70/70 kırmızı**. Bulgu → karşılığı:
>
> | Bulgu | Neydi | Karşılığı (test) | Kıran mutasyon |
> |---|---|---|---|
> | B1 | Tekrar korumasının kablolaması: `op_open_session`/`op_complete_enrollment`'a eşleşen adım yerine Go'nun o anki adımı verilince paket yeşil, tek kod 2 oturum | `TestTOTP_ANextStepCodeIsRetiredByItsOwnStep`, `TestEnrollment_ANextStepFirstCodeCannotSignInAgain` (bir SONRAKİ adımın kodu: saklanan adım = eşleşen adım; Go saati o adıma geçince aynı kod `ErrCodeRejected`, 1 oturum) | X8, X8b |
> | B2 | Audit tavanı yalnız `unknown_email` ile sürülüyordu | `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter`: üç tür, her biri önce kontrol (1 satır), sonra tavan tükenmişken 0 satır | X2, X3 |
> | B3 | Harici sızıntı testi TOTP'u doğrulanmayan challenge'la sürüyordu; kod hiç kullanılmıyordu | `TestLeak_NoInputInAnyErrorOrLogLine` yeniden yazıldı: challenge'ı parola adımı basar; kod kolları (yanlış kod, kilit, DB reddi, DB hatası, satır yazılamaması) ve enrollment kolları sentinel'iyle doğrulanarak sürülür. ~~her giriş noktasının her hata kolu~~ — **3. turda yanlışlandı:** throttle kolları, hesap-gitmiş, zarf ve biçimsiz oturum token'ı kolları ulaşılmıyordu; kolların numaralı listesi 3. tur tablosunda | X11, X15, X16 (+ X10, Y39) |
> | B4 | Enrollment bütçesinin bcrypt'ten önce harcandığı pinsiz | aynı test: reddedilen istekte 0 digest, 0 DB çağrısı (`digestFn` sayacı, `countingStore`); kontrol 1 + 1 | Y20 |
> | N1 | Oturum hash'inin anahtarı pinsiz | `storedUnderTokenKey` — satırdaki `token_hash` = HMAC-SHA256(TokenHMACKey, token), testte bağımsız hesaplanır (boolean): giriş ve enrollment oturumu | X1, X1b |
> | N2 | Çerez testi alt dize arıyordu | `TestCookies_AreHostPrefixedStrictAndSecure`: nitelik KÜMESİ tam eşitlik, ad ve değer tam | X4, X5 |
> | N3 | Harness'in `tappa_operator` kimliği pinsiz | `isOperator`: her çağrıda `current_user` = `session_user` = `tappa_operator` | X6 |
> | N4 | Token biçim kapısı pinsiz | `TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens`: her ret vakası ödediği digest ve DB çağrısını sayar (Go'nun kendi reddi 0/0, DB reddi 1/1) | Y39 |
> | N5 | "Hash dizge üzerinden, injektif" iddiası pinsiz | Ölçüldü, iddia doğru ve yük taşıyor: 32 baytın son base64 karakterinde 2 kullanılmayan bit var ve biçim kapısı gevşek çözer; aynı baytların ikinci yazımı çözülmüş baytlarla hash'lenseydi aynı oturumun ikinci geçerli çerezi olurdu → `TestSessionToken_HashIsKeyedLowerHexOverTheString` | Y13 |
> | N6 | `challengeMACLabel = ""` yeşil; sıfır id'li challenge | `TestChallenge_KeyIsDerivedNotTheSessionKey`: etiketin literal yazımıyla MAC kabul, etiketsiz MAC ret; sıfır id'li imzalı challenge ret | X7, X9 |
> | N7 | `limits.go`'nun enroll yorumu yanlış (dağıtık sel değil, tek adres) | Yorum, md. 8 ve OP-8 notu düzeltildi (ölçüm md. 8'de); sayı değişmedi, öneri OP-8'e | — (yorum) |
> | N8 | Saklanamayan adres 22021 → DB hatası, "aynı yanıt" kümesinin dışında | `internal/db`: aşırı uzun adres yolu; `TestPassword_EveryArmPaysOneComparisonAtTheSameCost` iki kol, `TestOperatorAccessors_AnUnstorableAddressIsNoAnswerNotAnError`, kuşak testine iki sonda | N8a, N8b |
> | N9 | Sayılar | Kapsam yukarıda tek kaynak; satır bedeli md. 8'de iki ölçümü kapsayan bant; tablo satırında "8 s" → "8 sa"; test sayısı yukarıda | — |
>
> **3. tur (2026-09-30) — 2. denetçi (bağımsız) RED: 1. turun 13 bulgusunun kapandığını
> doğruladı, 2 bloklayıcı + 5 bloklamayan buldu; hepsi kapatıldı.** Denetçi sondaları ve
> probe dosyaları bu turda repoya **konmadı** — 2. turda repoya geçici bir probe dosyası koymak bir
> kısıt sapmasıydı (iz kalmadı, doğrulandı); bu turda denetçinin enrollment sondası repoyu
> kopyaladığım scratchpad dizininde koştu. Bulgu → karşılığı:
>
> | Bulgu | Neydi | Karşılığı (test) | Kıran mutasyon ve mesaj |
> |---|---|---|---|
> | B-1 | `digestFn` üretim maliyet pinini kaldırmıştı: testler alanı `hashPassword`'le eziyordu, `cost != Cost` testin kurduğu fonksiyonu ölçüyordu; cost-13/14 üreten üretim `digestFn`'i yeşil (kayıtlı operatörün karşılaştırması sahte digest'inkinden pahalı → "aynı süre" kırılır, e-posta kehaneti) | İki test de ÜRETİM değerini sarmalar (`orig := a.digestFn`); `TestPassword_TheDigestAndTheDummyAreBothCostTwelve` kayıtlı digest'i `New`'un kurduğu `Authenticator`'ın kendi `digestFn`'iyle üretir; `TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens` maliyeti DB satırındaki digest'ten (`bcrypt.Cost`) okur | M1: "cost=13 …" ve "stored digest: cost 13 …, want 12" · M1b: cost 14 · B1a (kendi) |
> | B-2 | Sızıntı testinin "her kol" iddiası: throttle kolları, hesap-gitmiş, zarf ve biçimsiz token kolları ulaşılmıyordu | `TestLeak_NoInputInAnyErrorOrLogLine` kolları **numaralı** sürer ve başlığında sayar: P1–P6, T1–T12, V1–V3, L1–L3, E1–E9, A1, N1 — throttle'lar tekrarlı çağrıyla (work bütçesi parola kuralında reddedilen ucuz isteklerle; süreç geneli enrollment bütçesi Go'nun her kontrolünü geçen isteklerle), zarf kolu başka KEK altında mühürlü hesapla, hesap-gitmiş kolu geçerli challenge + boş store'la, A1 audit tavanını doldurarak. **Ulaşılmayan, sayılı:** `crypto/rand`, bcrypt üretimi, `Seal` ve token hash hatalarının kolları (dışarıdan başarısız kılınamaz) | M3, M3b, M4, M5, M5b, M6, M6b; kendi LK1–LK4 (biçimsiz token kolu değeri taşır ×2, hesap-gitmiş ve hesap-throttle kolları kodu taşır) |
> | N-a | N7'nin 2. tur cümleleri de yanlıştı ("hiçbir satır yazılmaz", "yeni link verir", "pencere boyunca") | Yorum, md. 8 ve OP-8 notu ölçülen gerçekle düzeltildi: pencere başına 10 `enrollment_failed` satırı, yeni link kaçış değil, saldırı süresiz ~~dakikada ~1 istekle … sayacı yalnız süreç yeniden başlatması sıfırlar~~ (4. turda mekanizma düzeltildi: pencere başında 10'luk patlama, sıfırlanma pencerenin kendisi) (denetçinin sondası scratchpad kopyasında yeniden koşuldu: 10 red / 10 satır, meşru → throttled, yeni link → throttled, 2. pencere aynı) | — (metin) |
> | N-b | OP-9 devir işaretçisi eksikti | OP-4 bloğunun OP-9 maddesine token biçimi şartı ve hash fonksiyonu adıyla eklendi | — (metin) |
> | N-c | "Hesap bütçesi hesaba dokunmadan önce" pinsizdi | `countingStore.byID`: bütçenin reddettiği denemede hesap **okunmaz** (`TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter`) | M11: "past the account budget the account was read 2 time(s), want 0" |
> | N-d | Sızıntı testi yalnız `Error()` metnini tarıyordu | Her hata ayrıca `%v`, `%+v`, `%#v` ile de taranır | — (tarama genişledi) |
> | N-e | md. 17'nin süreleri | md. 17'de tam koşuların aralığı (4. turda yedi: beşi bu doğrulamanın, ikisi 2. denetçinin), "yüke bağlı gözlem" diye | — |
>
> **3. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** bu kartın
> kendi listesi 70 → **75** (sızıntı testinin yeni kollarını taşıyan dört mutasyon ve B-1'in
> kendi biçimi) — **75/75 kırmızı**; 1. denetçinin 25'i — **25/25 kırmızı**; 2. denetçinin 62'si
> (56 + 6) — **62/62 kırmızı**. 2. denetçinin `C-M1`/`M1-vs-fixed` çifti koşulmadı: iki
> mutasyonun metni tam olarak bu turun B-1 düzeltmesidir (testin üretim `digestFn`'ini
> sarmalaması), artık uygulanacak kalıp yok. **Kendi hatam, kapı zincirinde yakalandı:** bu turda
> sızıntı testine eklediğim bir sabit (sır kelimesi + üç karakter sınıflı değer aynı satırda)
> `redline` R7d'yi `a0-token` sınıfıyla kırmızıya çevirdi; sabit yeniden adlandırıldı, R7d
> muafiyeti eklenmedi, `redline` rc=0.
>
> **4. tur (2026-09-30) — 3. denetçi (bağımsız) RED: 2. turun 7 bulgusunun kapandığını ve B-2'nin
> numaralı kollarının 34/34 kırmızı olduğunu doğruladı; 3 bloklayıcı + 3 bloklamayan buldu; hepsi
> kapatıldı.** Denetçinin 55 mutasyonu aynı adlarla bu ağaçta yeniden koşuldu (sonuç aşağıda);
> sonda/probe repoya konmadı. Bulgu → karşılığı:
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B-A | Sızıntı testinin arama kümesi elle listelenmişti: enrollment'ın düz TOTP sırrı hex'te, E4'ün sayfaları, E3'ün biçimsiz token'ı, oturum hash'i, yeni digest ve yeni zarf, 31 baytlık KEK kümede yoktu | ~~Küme artık **türetilir**: verilen her girdi (…), sahte store'un döndürdüğü (…) ve **aldığı** her kimlik bilgisi (…), türetilenler (…)~~ — **5. turda yanlışlandı** (4. denetçi: iddia kapsamdan genişti — `reveal()` ile T12/E9'a konan ham token, türetilmiş challenge anahtarı, bir sonraki adımın kodu, `% x` ve `%#v` render'ları, bölünmüş kod yakalanmıyordu; sahte store'un hasadı da pinsizdi, silinen bir kayıt yeşil kalıyordu). Yerine **numaralı sözleşme** geçti: 5. tur alt bölümü. 4. turun ölçtüğü kısım: bu satırdaki değerler kümeye eklendi, her üye 12 render'da aranır (ham, `%q` ve JSON içi, hex küçük/büyük, base32 dolgulu/dolgusuz, base64 std/url ham/dolgulu, `[]byte`'ın `%v`'si); 6 karakterden kısa render aranmaz (en kısa üyeler 6 haneli kodlardır ve ilk sürümdeki gibi kümede kalır: paketin sabit metinlerinde altı rakamlık dizi yok). **Üye başına pozitif kontrol:** her üye 8 fiil/kodlamayla bir hataya ve Debug log satırına konur, aynı arama bulmak zorunda | G-E5hex, G-E4blob, G-E3tok, G-V3hash, G-T12hash, G-E9digest, G-E9sealed, G-N1kek (+ G-P1addr: adresler de kümede) |
> | B-B | Log yakalama Info seviyesindeydi | Sızıntı testinin yakalayıcısı **Debug** seviyesinde ve üretimin iki handler'ıyla (text + JSON, `cmd/tappa` `logHandler`); paket içi `newTestLogger` da Debug | G-debug |
> | B-C | Audit tavanı tür başına pinliydi, kol başına değil | (1) `TestAuditRows_EveryPasswordlessKindGoesThroughTheCap`: kaynaktan (AST) her `RecordOperatorAuthEvent` doğrudan çağrısının türü yalnız `totp_failed`/`locked` (değişkenle verilen tür fonksiyondaki bütün atamalarına çözülür), her `recordPasswordless` çağrısının türü yalnız parolasız üç tür; kendi pozitif kontrolü üç mutant (tavanın etrafından enrollment_failed, tavandan totp_failed, değişken türün login_failed yapılması). (2) `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter` enrollment_failed'ı kimliksiz ulaşılabilen dört koluyla sürer: E3, E4, E5, E8 — her biri tavanın altında 1 satır, tavan tükenmişken 0 | C-E4bypass, C-E5bypass, C-E8bypass (kontrol C-E3bypassCtl) |
> | N-1 | "Her giriş noktası" iddiasının dışında kalan kollar | Numaralı listeye eklendi ve sürüldü: B1 `BeginEnrollment(uuid.Nil)`, C1/C2 çerez setter'larının boş değer reddi, ~~R1–R4~~ K1–K4 *(7. tur: render'ların R1–R15'iyle çakışıyordu, yeniden adlandırıldı)* okuyucuların yok/boş çerez kolları | — |
> | N-2 | Enrollment bütçesi saldırısının mekanizması yanlış anlatılıyordu ("dakikada ~1 istek", "yalnız yeniden başlatma sıfırlar") | Yorum, md. 8 ve OP-8 notu: sabit pencere, sayaç pencere dolunca kendiliğinden sıfırlanır; sürekli ret her pencerenin başında 10'luk patlama ister; eşit yayılmış istekler bütçeyi pencerenin çoğunda açık bırakır | — (metin) |
> | N-3 | İstemci adresi sızıntı yüzeyi | OP-8 kabul listesine adıyla: adresin handler log/hata yüzeyinde ele alınışı OP-8'in kararı; OP-8'in sızıntı testi Debug + iki handler + türetilmiş küme | — |
>
> **4. tur mutasyon koşuları:** 3. denetçinin 55'i — **55/55 kırmızı** (G-* ve C-* dahil; mesajlar
> teslim raporunda); bu kartın 75'i — **75/75**; 1. denetçinin 25'i — **25/25**; 2. denetçinin 62'si
> — **62/62**. Yarış testi düzeltmesinden sonra ona dayanan F1, F18, M30, M41 ve Y45 yeniden
> koşuldu — hâlâ kırmızı. Tam koşu (`.env`, `-race`): o koşuda tek kırmızı T72. *(5. tur
> notu: tam koşu deterministik değildir — 4. denetçinin 1. koşusunda `internal/db`
> `TestConsumeInvite_ConcurrentRaceExactlyOneWinner` 53300 verdi, T34 sınıfı; "tek kırmızı
> T72" her koşu için ayrı yazılır.)*
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | (kendi) | Yarış testi zamanlamaya bağlı kırmızıydı: 7 ret kilit eşiğini (5) aşar, hesabı geç okuyan kaybeden `ErrLocked` alır. Bu turun ilk paket koşusunda bir kez (çıktı yakalanmadan) ve mutasyon koşularında iki kez (G-E3tok, X8 — ikisi de başka testlerle de kırmızı) görüldü: ~125 paket koşusunda 3 | `TestTOTP_SameCodeFromNGoroutinesOpensExactlyOneSession` artık zamanlamadan bağımsız olanı ister (yukarıda md. 11); F1, F18, M30, M41, Y45 yeniden koşuldu, hâlâ kırmızı | — |
>
> **5. tur (2026-09-30) — 4. denetçi (bağımsız) RED: ürün kodunda kusur yok; bloklayıcı sınıf
> DÖRDÜNCÜ kez geldi — sızıntı testinin yazılı iddiası gerçek kapsamından genişti.** Denetçinin
> mutasyonları (`D1`–`D9`, `D4b`, `H1`, `H2`, `E1a`, `E2`, `R3`, `L1` ve 4. tur listesinin geri
> kalanı) bu ağaçta aynı adlarla koşuldu; sonda/probe repoya konmadı. Orkestratörün kararı (M8-02
> FAZ C dersi: *"yama isteme — karar iste"*): iddia **sayılı ve grep'le çözülebilir bir sözleşmeye**
> indirildi. Üç yol ölçüldü:
>
> - **(a) Yapısal kapatma (redacting tipler) — elendi, OP-8'e öneri.** Yüzey sayıldı (`go doc`):
>   düz `string` olarak giren kimlik bilgisi OP-8'in API'sinde 6 parametre (`Password`'ün e-posta
>   ve parolası; `TOTP`'un kodu; `CompleteEnrollment`'ın ham token'ı, parolası, kodu), OP-7'nin
>   kablolamasında 2 `[]byte` alan (`Config.TOTPKEK`, `Config.TokenHMACKey`), `Store` arayüzünün
>   6 metodunda 9 parametre (e-posta ×2, oturum hash'i ×4, ham token, yeni digest, yeni zarf —
>   `internal/db`'nin imzaları), pakette 4 iç anahtar alanı (`kek`, `tokenKey`, `challengeKey`,
>   `dummyDigest`). Kapattığı sınıf: değer o noktada sarmalayıcı tipte tutulurken **kazara bir
>   biçim fiili** — D1, D2, D5, D7, D8 (analiz). Kapatmadığı, **ölçüldü**: D4, D4b ve D9 ham oturum
>   token'ını `SessionToken`'dan sızdırır — o tip **bugün zaten** sarmalayıcıdır, mutasyon
>   `tok.reveal()`'la geçer; D6 yeni HESAPLANAN bir değerdir (bir sonraki adımın kodu), hiçbir tip
>   onu tutmaz; D3 bir dizgeyi böler. Bedeli üç görevin API'si (OP-7, OP-8, `internal/db`) — bu
>   görevin sınırı dışında; kara kutu aramasının YERİNE değil, önüne konacak bir katman olarak
>   OP-8'e öneri.
> - **(b) İddiayı gerçeğe indirmek — SEÇİLDİ** (orkestratörün önerisi, aşağıda).
> - **(c) Hükmü kaldırmak (kara kutu testini silmek) — elendi, ölçüldü.** Sızıntı sınıfındaki
>   96 mutasyon (bu turun koşularında `TestLeak_NoInputInAnyErrorOrLogLine`'ın kırmızısı
>   arasında olduğu her biri; testin kendi metnini değiştirenler hariç) paketin tamamıyla, yalnız o
>   test `-skip` ile atlanarak yeniden koşuldu: **77'si YEŞİL** — onları yalnız kara kutu
>   testi öldürüyor (4. denetçinin 18'i — D1, D2, D4, D4b, D5, D6, D7, D8, D9 dahil; 3. denetçinin 42'si — A-* ve G-* kollarından; 2. denetçinin 11'i; 1. denetçinin 3'ü — X11, X15, X16; bu kartın LK1, LK2, LK4'ü). Kalan 19'u başka testler de kırmızıya çeviriyor. Silmek bunları görünmez yapar.
>
> **Sözleşme — tek kaynağı testin kendisidir** (`internal/operatorauth/leak_external_test.go`:
> `TestLeak_NoInputInAnyErrorOrLogLine`'ın başlığı ve sabitleri; kart kopyalamaz, kopya kayar).
> Biçimi: üye GRUPLARI numaralı (`G1`–`G17`, her biri kaynağıyla); **kapalı ölçüt** `neverLog` —
> CLAUDE.md §7 + ADR 0020 §5 + ADR 0021 §3.5'in bu pakette var olan her "asla loglanmaz"
> maddesi (`N1`–`N15`: oturum token'ı ve hash'i · TOTP kodu, o anki **ve** ±1 · TOTP sırrı ·
> enrollment token'ı ve hash'i · parola/yeni parola · digest · zarf · bekleyen blob · KEK · token
> HMAC anahtarı · türetilmiş challenge anahtarı · challenge · operatör adresi), literal tablo,
> uzunluğu pinli, her maddenin **her grubunda** ≥1 üye zorunlu (`N3` iki grup: o anki kod ve ±1
> kodları); RENDER'LAR numaralı (`R1`–`R15`; `% x`/`% X` ve `%#v` bu turda eklendi); KOLLAR
> numaralı; hatalar `Error()`/`%v`/`%+v`/`%#v`, log Debug'da iki handler'la; testin hiç görmediği
> ham oturum token'ları, sahte store'un aldığı hash'lerin **ön görüntüsü** olarak aranır
> (43 karakterlik base64url pencerelerinin HMAC'i). **Aranmayanlar, ADIYLA:** bölünmüş/kısmi
> değerler (D3); listede olmayan render'lar — **ölçülerek** (yedi örnek değer): `%+q` (ASCII
> olmayan bayt), `% #x`, bayt başına `%b`/`%o`, hex alfabeli base32, ascii85, MIME satırlı
> base64, ayrılmış/ASCII olmayan baytın URL kaçışı, `<>&'"` HTML kaçışı, ters bayt sırası,
> `[N]uint8` dizisinin `%#v`'si, harf katlaması (ve listede olmayıp yine de yakalananlar:
> `%#q`, `%#x`, `[]byte`'ın `%s`/`%d`'si, `[N]byte`'ın `%v`'si, slog text/JSON'un `[]byte`'ı,
> `json.Marshal`); bu pakette olmayan maddeler (CMAC, davet kodu, tam GPS, okuma bileti);
> dışarıdan başarısız kılınamayan kollar (`crypto/rand`, bcrypt üretimi, `Seal`, token hash).
> **Hasat bütünlüğü:** 40'lık gevşek taban kalktı; sahte store'un kaydeden her metodu çağrısını bir
> ifadede sayar, vektörünü başka bir ifadede kaydeder, test metot metot `çağrı > 0`, `kayıt =
> çağrı` ve her vektörün aritesini ister (`storeArity`).
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B1 · D4, D4b (+ D9) | `tok.reveal()` ile T12/E9'un hatasına (D9: T9'da Debug log'a) konan ham oturum token'ı — test onu hiç görmüyordu | Ön görüntü araması: hata ve log metnindeki her 43 karakterlik base64url penceresi (R1–R15'in tersleriyle çözülerek) token anahtarıyla HMAC'lenir, sahte store'un aldığı hash'lerle karşılaştırılır; pozitif kontrolü sentetik bir token | D4, D4b, D9 |
> | B1 · D5 | Türetilmiş challenge anahtarı kümede yoktu | `G13` = HMAC(token anahtarı, etiket), testte hesaplanır; etiketin literal yazımı `TestChallenge_KeyIsDerivedNotTheSessionKey`'de pinli | D5; KL (etiket değişince birim testi kırmızı) |
> | B1 · D6 | ±1 adımın kodu kümede yoktu | `G17` (her iki sır için önceki ve sonraki adımın kodu) ve `N3`'ün iki gruba bağlanması | D6; NL3 (±1 kodları kümeden çıkınca kapalı ölçüt kırmızı), NL3×D6 |
> | B1 · D1, D8 | `% x` render'ı aranmıyordu | `R6`/`R7` | D1, D8 |
> | B1 · D2, D7 | `%#v` render'ı aranmıyordu | `R15` | D2, D7 |
> | B1 · D3 | Bölünmüş kod ("123-456") | Aranmayanlar listesinde adıyla | D3 **yeşil — beklenen** |
> | B1 · H1, H2 | Hasat pinsizdi (40'lık taban, 80 üye) | Metot başına pin (`storeArity`) | H1, H2; kendi H4 (çağrı ne sayıldı ne kaydedildi), H5 (arity eksik), H6 (adres kaydı düştü); NL1 (kümeden bir madde düştü), NL2 (`neverLog`'dan satır silindi) |
> | N1 | AST testi yöntem değerini görmüyordu (`rec := a.store.RecordOperatorAuthEvent; rec(…)`) | Çağrının `Fun`'ı olmayan her `RecordOperatorAuthEvent` seçicisi ihlal; testin kendi pozitif kontrolüne dördüncü mutant. **Sayılı sınır:** okuma ADA göredir — başka adla satır yazan yeni bir `Store` metodu ya da reflection görülmez | E1a, E2 (+ E1b, E3) |
> | N2 · R3 | Yarış testi "`locked` yalnız ≥5 `totp_failed`'dan sonra"yı son sayıyla ölçüyordu | Satırlar `ORDER BY at, id` ile okunur, ilk `locked`'dan önce ≥5 `totp_failed` (md. 11) | R3 (mesaj: "a 'locked' row was written after only 0 totp_failed row(s)") |
> | N2 · L1 | Etiketi pencerenin bitişine bakmadan kuran mutasyon bütün pakette yeşildi | `TestLock_ThresholdAndWindowThroughTheSignIn`'e pencere sonrası vaka (md. 10) | L1 (mesaj: "a refused code after the window: … the account is locked for a while, want ErrCodeRejected") |
> | N3 | "14 `locked`" yanlıştı (31); kayan sayılar | Doğrulama bloğu md. 5'ten sayılar kaldırıldı, kayan sayı yazılmıyor | — |
> | N4 | "Tek kırmızı T72" tam koşu için genelleniyordu | Her koşu ayrı yazılır; 4. denetçinin 53300'ü (T34 sınıfı) 4. tur notunda | — |
> | N5 | "N-1 'totp_failed' rows" yorumu ve md. 11'in ilk cümlesi | İkisi de "7 başarısız deneme satırı — `totp_failed`, zamanlamaya göre birkaçı `locked`" | — |
> | (kendi) | Bu turun L1 vakası ilk yazımında zamanlamaya bağlıydı: pencerenin bitişi yalnız veritabanının saatiyle yazılmıştı, testin enjekte saati adımın ortası — veritabanının saati adımın ikinci yarısındaysa Go hesabı hâlâ kilitli okur. Mutasyon koşularında L1 dışındaki 11 paket koşusunun 6'sında `TestLock_ThresholdAndWindowThroughTheSignIn` kırmızısı olarak göründü (beşi başka testlerle de kırmızıydı; D3'ün TEK kırmızısı buydu — yanlış bir kırmızı; D3 düzeltmeden sonra yeniden koşuldu: yeşil, beklenen). Denetçinin 38'i düzeltmeden sonra baştan yeniden koşuldu; tablolardaki sonuçlar o koşunun | Bitiş iki saatin küçüğünden 1 sn önce (`least(clock_timestamp(), now)`); ölçüldü: eski biçim 300 ardışık koşunun 95'inde kırmızı, yenisi 0/300 (koşular bir tam 30 sn adımı kapsadı) | — |
>
> **5. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** 4. denetçinin
> 38'i — **35 kırmızı, 3 yeşil ve üçü de beklenen:** D3 (aranmayanlar listesinde adıyla), F1
> (denetçinin kontrolü: geç okuyan kaybedenleri zorlar, testin zamanlamaya dayanıklı olduğunu
> gösterir) ve BB2 (audit tavanının WARN satırına e-posta — kara kutu testinin A1 kolu tavanı
> e-postasız türle aşar; aynı mutasyon paketin tamamında, BB2full, `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter` ile kırmızı:
> *"carries the email=true"*); D6'nın denetçinin ilk listesindeki biçimi derlenmiyordu (`step`
> yerel değişkeni fonksiyonu gölgeliyor), ikinci listesindeki biçimi koşuldu. Denetçinin H3'ü
> (40'lık tabanın ölçüm sondası) ve F2'si (eski test beklentisi) koşulmadı: uygulanacak metin artık
> yok. Bu kartın listesi 75 → **83** (H4, H5, H6, NL1, NL2, NL3, NL3×D6, KL) — **83/83 kırmızı**;
> 1. denetçinin 25'i — **25/25**; 2. denetçinin 62'si — **62/62**; 3. denetçinin 55'i — **55/55**.
> Tam koşu (`.env`, `-race -count=1 ./...`, 2026-09-30 19:10 UTC, yük 2,6–4,3): **bu koşuda** tek
> kırmızı T72 (`cmd/rotatekek`); `internal/operatorauth` 168,1 sn, `internal/db` 103,8 sn, duvar
> 280 sn. Kapı zinciri ve kapsam yukarıda (doğrulama bloğu md. 2 ve 4).
>
> **6. tur (2026-09-30) — 5. denetçi (bağımsız) RED: 4. denetçinin bulgularının hepsini doğru
> testle ve doğru sebeple kapanmış buldu; sözleşmeyi doğruladı (G1–G17 üyeleri, 33 kol tek tek
> sızdırıldı, aranmayanlar listesi, `neverLog`'un ADR/CLAUDE.md listelerine göre tamlığı,
> `TestLock_ThresholdAndWindowThroughTheSignIn` 350/350, yarış testi 20/20). Tek bloklayıcı
> aynı sınıftan: aynı dosyada kalan iki evrensel iddia — `specimens`'ın *"every secret-bearing
> type this package … exposes"*'u ve yapısal testin *"a type this package hands out"*'u; oysa
> dışa açık, anahtar taşıyan iki tip (`*Authenticator`, `Config`) hiçbir listede yoktu ve
> ikisi de anahtarları basıyordu.** Orkestratörün kararı: ikisini birden — gerçek açığı kapat,
> iddiayı kapalı kümeye bağla.
>
> **Ürün değişikliği (ADR 0020 §2'ye tarihli not):**
>
> - `*Authenticator` ve `Config` token tiplerinin beş yöntemini taşır (`Format`, `String`,
>   `GoString`, `LogValue`, `MarshalText`; değer alıcı, yani kopyalanmış bir değer de redakte
>   eder); yer tutucular `operatorauth.Authenticator(redacted)` ve `operatorauth.Config(redacted)`.
>   **Ölçüldü** (scratchpad kopyasında sonda, repoya konmadı): yöntemlerle `%v`/`%+v`/`%#v`/`%s`/
>   `%x`/`%q`/`%d`, `[]any` içinde, slog text ve JSON, `json.Marshal` (doğrudan ve dışa açık alanda)
>   — anahtar yok. `String`/`GoString`: `Format` varken fmt onları hiç çağırmaz; doğrudan
>   `Stringer` isteyen tüketici için tutuldu ve beşi de pinli. JSON: `MarshalText` olmadan
>   `*Authenticator` `{}` basıyordu, `Config` func alanı yüzünden hata veriyordu — tesadüf;
>   şimdi ikisi de yer tutucu.
> - **Yöntemlerin ulaşamadığı yol, ölçüldü:** çağıranın DIŞA KAPALI alanında tutulan bir DEĞER
>   (fmt yöntemi çağıramaz, yansımayla yazdırır). ~~`Authenticator` için **kapatıldı**: dört
>   anahtar alanı … bir işaretçinin (`keys *authKeys`) arkasına taşındı — … yansıma adres
>   basar.~~ **7. turda yanlışlandı (6. denetçi, ölçümle):** yalnız `%v`-ailesinde adres
>   basıyordu; `%s %q %e %f %t %c %U`'da fmt'nin `badVerb`'ü struct'ı gösteren işaretçiyi bir
>   kez açar ve dört anahtarın dördü de basıldı — 7. tur alt bölümü. `Config` için
>   **kapatılmadı, adıyla**: anahtar alanları OP-7'nin yazdığı dışa açık `[]byte` alanlarıdır;
>   kapatmak onları bir işaretçinin (ya da redakte eden bir anahtar tipinin) arkasına, yani
>   OP-7'nin API'sine taşır. Sayılı sınır (md. 18) + OP-7 kuralı (OP-4 bloğu, OP-7 listesi:
>   `Config` doğrudan `New`'a gider, dışa kapalı alanda değer olarak tutulmaz); sızıntı testinin
>   `knownLeaks`'i bu dört yolu adıyla tutar ve iki yönden pinler (sızmayı bırakan giriş de
>   kırmızı). API kırılması yok; OP-7'ye etkisi bu kural. *(7. turda yanlışlandı: yol "tek"
>   değildi — `%p` ve `%w` da basıyordu, dışa kapalı alan yolu her fiilde — ve "OP-7'nin
>   API'sini değiştirir" gerçek bir maliyet değildi, OP-7 henüz yazılmadı; `Config` artık
>   `Key` taşır, `knownLeaks` kaldırıldı.)*
>
> **İddia — kapalı tip kümesi** (`internal/operatorauth/leak_external_test.go`):
>
> - `declaredTypes`: paketin test dışı dosyalarında dışa açık her tip, `go/parser` ile KAYNAKTAN.
>   `reachableDBTypes`: bu tiplerden yansımayla ulaşılan her `internal/db` tipi (dışa açık alanlar,
>   dışa açık yöntem imzaları, arayüz yöntemleri; yalnız bu paketin ve `internal/db`'nin tipleri
>   yürünür) — db tipleri için sayılı sınıra yazmak yerine aynı kümeye almak ucuzdu (tek yürüyüş
>   fonksiyonu); `db.PasswordHash` bu yüzden yeni bir specimen. *(7. tur: ikisi de `apiTypes`'a
>   — iki paketin tip denetimine — dönüştü; 6. denetçinin ölçtüğü üç kaçış — `db.*` taşıyan
>   dışa açık fonksiyon/değişken, dışa kapalı tip döndüren dışa açık yöntem, fonksiyon içi tip
>   — yansıma yürüyüşünde görülmüyordu.)*
> - `TestExportedTypes_EveryOneIsASpecimenOrANamedException`: kümenin her tipi ya `specimens`'ta
>   (doldurulmuş bir örnekle yazdırma yolları taranır) ya `notSpecimens`'ta (adıyla ve gerekçesiyle)
>   — ikisi birden değil; sınıflandırılmamış tip ve kümede olmayan bir giriş kırmızı. *(7. turda
>   yanlışlandı, `db.*` için: istisnalar yürüyüşün köküydü, ulaşılamayan bir `db.*` girişi
>   kendini ulaşılabilir kılıyordu — 6. denetçi iki tane ekledi, test yeşil kaldı. 7. turda
>   kökler yalnız paketin kendi bildirimleri.)*
> - `TestExportedTypes_CarryNoPlainStringField` artık üç elle seçilmiş tipe değil kümeye bakar:
>   kümenin her tipinin ve tuttuğu bu paketin/`internal/db`'nin struct'larının (değer ve işaretçi
>   üzerinden yürünerek) her alanı `allowedFields`'ta gerekçesiyle (işlevler ve `okFieldTypes`
>   hariç); artık var olmayan bir alanı adlandıran giriş de kırmızı. *(7. turda düzeltildi: bu
>   paketin ya da db'nin struct tipindeki alanlar adlandırılmıyor, yalnız içlerine iniliyordu —
>   `Authenticator`'a `cfg Config` eklemek yeşildi; artık onlar da adlandırılır.)*
> - `TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder` elle listeye değil kümenin
>   kendini biçimlendiren her tipine bakar.
> - Redakte eden her specimen, yöntemlerine ulaşılan her yolda yer tutucusunu basmak ve beş
>   yöntemi yer tutucuyu döndürmek zorunda; slog değeri string'e çözmeli (`LogValue`). Bu pin
>   olmadan bir yöntemi silmek yeşil kalabiliyordu: `%d`'ye yalnız `Format` ulaşır, `LogValue`
>   yoksa slog handler'ı `MarshalText`'e düşer.
> - İşaretçiler: bayt değerleri için ondalık liste (5. denetçinin ölçtüğü biçim), `%#v` listesi,
>   `%q`'nun kaçışlı metni (bayt kontrolü ölçtü: rastgele baytlar kaçışlanır) ve base64; yollar:
>   `%d` ve "dışa kapalı alanda, işaret edilen değer". Bayt değerleri için ayrı pozitif kontrol
>   (`bareBytes`).
> - "every secret-bearing type this package … exposes" ve "a type this package hands out"
>   kaldırıldı; dosyada kalan "every" ifadeleri AST/yansıma kümesi, sabit bir tablo ya da numaralı
>   kollar üzerinden.
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B1 | İki evrensel iddia; `*Authenticator` ve `Config` hiçbir listede yoktu, `%v`/`%+v`/slog text'te anahtarları basıyordu; `lastCode string` eklenince iki test de yeşil | Yukarıda: beş yöntem + `Authenticator` anahtarları işaretçi arkasında + kapalı tip kümesi + alan düzeyi kapalı liste | B1-lastCode (denetçinin K-ExportedTypes'i): *"Authenticator.lastCode is a field nothing authorised"* · `Format`/`LogValue`/`MarshalText`/`String`/`GoString` silinmesi (iki tip, yöntem ve derleme-zamanı iddiası birlikte), ör. *"Config leaks on %d"*, *"slog resolves it to a Any, not to its placeholder (LogValue)"* · B1-keysByValue (anahtarlar işaretçisiz): *"Authenticator leaks on unexported field, pointee %+v"* · sınıflandırılmamış yeni tip ×2: *"exported type Note is neither a specimen nor a named exception"* · db specimen'i düşürmek, istisna düşürmek, bayat `allowedFields` ve bayat `knownLeaks` girişi |
> | N1 | R3'ün kendi pozitif kontrolü yoktu; R3 boşaltılınca test yeşil | Her render adıyla kendi builder'ına eşlenir (literal tablo; fmt fiili ya da kodlayıcının akış yazıcısı — render'ın kendi fonksiyonu değil); her üye için render'ın iğnesi aranıyor olmalı ve builder'ın metninde geçmeli | N1-R3-empty ve denetçinin K-R3off'u: *"R3 JSON string inside gives member 0 (G11 TOTP KEK) no searched needle"* · N1-R3-raw (JSON kaçışı olmadan): *"…needle for member 5 (G13 challenge MAC key) is not in its builder's text"* · K-R3json+off |
> | N2 | AST testi yalnız `FuncDecl` gövdelerini geziyordu; paket düzeyi func literal görülmüyordu | Her dosyanın her bildirimi gezilir (`GenDecl` kendi kapsamı); pozitif kontrole beşinci mutant — yalnız E4 kolunu paket düzeyi literale çeviren (ilk yazdığım mutant bütün tavan çağrısını kaldırıyordu ve "hiçbir tür tavandan geçmiyor" denetimiyle yakalanıyordu, paket düzeyi yürüyüşü sınamıyordu — yürüyüşü geri alan mutasyon onunla yeşildi, bu yüzden daraltıldı). Sayılı sınırlar ADIYLA yorumda ve md. 18'de | N2-pkglevel-literal ve denetçinin K-ASTfunclit'i (yalnız AST testiyle) · N2-walk-funcdecl-only (yürüyüş geri alınınca): *"POSITIVE CONTROL \"a package-level func literal around the cap\": the mutant was not flagged"* |
> | N3 | OP-7/OP-8 devir işaretçileri | OP-4 bloğu OP-7 listesi: redaksiyon var; anahtarlar redaksiyonsuz yapıya kopyalanmaz; `Config` dışa kapalı alanda değer olarak tutulmaz. OP-8 listesi: (a) redacting-tip önerisi (handler parametreleri), kapalı tip kümesi dersi | — |
> | N4 | OP-9 devrinde "veritabanına gitmeden" | Düzeltildi (iki yerde): biçimsiz token `op_complete_enrollment`'a ve digest'e gitmez, tavanın altında bir `enrollment_failed` satırı yazılır | — |
> | N5 | Kartta kısaltılmış test adları | Tam adlar yazıldı. `TestEveryNamedTestExists` neden yakalamadı: alıntı deseni `\bTest[A-Z][A-Za-z0-9_]{4,}\b` — `TestLock`'ta büyük harften sonra 3 karakter var, alıntı sayılmıyor; `TestLimits_…` `TestLimits_` olarak eşleşir ve `_` ile biten alıntı bir AİLE alıntısıdır, bir önek olarak çözülür (`TestDecide_` emsali) | — |
> | N6 | OP-8 devrinde "her grup kapalı bir ölçüte bağlı" | Düzeltildi: bağlılık maddeden gruba doğru; G16 bilerek hiçbir maddeye bağlı değil | — |
> | Not | Hasat pini içerik tutmuyor | Testin başlığındaki NOT CLAIMED listesinde ve md. 18'de adıyla | K-Hcontent/K-Hcontent2 yeşil, beklenen |
>
> **6. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** bu turun 20'si
> — **20/20 kırmızı**; bu kartın önceki listesi (75 + 5. turun 8'i) — **83/83**; 4. denetçinin
> 38'i — **35 kırmızı, 3 beklenen yeşil** (D3, F1, BB2 — 5. turla aynı); 1. denetçinin 25'i —
> **25/25**; 2. denetçinin 62'si — **62/62**; 3. denetçinin 55'i — **55/55**; **5. denetçinin 66'sı
> bu ağaçta aynı adlarla kuruldu** (sonda repoya konmadı; `a.challengeKey` → `a.keys.challengeKey`
> ve gofmt hizası uyarlandı) — **60 kırmızı, 5 yeşil, 1 derlenmeyen**: yeşillerin beşi de beklenen
> — B-D3 (adıyla aranmayan), BB2 (kara kutu testinde; BB2'nin paket düzeyi ikizi `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter`
> ile kırmızı), K-Hcontent2 (hasat içerik pinlemez — adıyla sayılı sınır), E-c ×2 ((c)
> ölçümünün kendisi: kara kutu testi atlanınca yeşil); derlenmeyen K-G17empty denetçinin ilk
> biçimi (`near` kullanılmaz kalır), düzeltilmiş ikizi K-G17empty2 kırmızı. 5. turda yeşil
> beklenen dördü artık kırmızı: K-R3off, K-R3json+off (N1), K-ASTfunclit (N2), K-ExportedTypes
> (B1). K-Hcontent (tek karakterlik sabit) da kırmızı, ama **yan yoldan**: N1'in yeni pini tek
> karakterlik bir üyenin hiçbir iğnesi olmadığını söyler — içerik pini değildir; 64 karakterlik
> ikizi (K-Hcontent2) yeşil kalır.
>
> **Kendi hatam, bu turda yakalandı:** N2'nin pozitif kontrolünü daraltırken paket düzeyi
> bildirimi mutanta ekleyen koşul eski metne bakıyordu (`HasPrefix(…, "recordAround")`); mutant
> bildirimsiz kaldı ve kontrol "flagged değil" diye kırmızıydı. Bu 18 mutasyon koşusunu yan yoldan
> kırmızı yaptı (4. denetçinin 11'i, 1.'nin 7'si); koşul düzeltildi ve 18'i yeniden koşuldu —
> tablodaki sonuçlar o koşunun (D3 yeniden yeşil, beklenen).
>
> Tam koşu (`.env`, `-race -count=1 ./...`, 2026-09-30 20:59 UTC, yük 2,0–4,9): **bu koşuda** tek
> kırmızı T72 (`cmd/rotatekek`); `internal/operatorauth` 180,4 sn, `internal/db` 42,5 sn, duvar
> 297 sn. Kapsam ve kapı zinciri yukarıda (doğrulama bloğu md. 2 ve 4). Tam koşudan sonra tek
> değişiklik: `Config`'in belge yorumuna sayılı sınır notu (yalnız yorum; ardından build, vet,
> `gofmt -s`, redline, `TestEveryNamedTestExists` ve paketin kendisi yeniden koşuldu, yeşil).
>
> **7. tur (2026-09-30) — 6. denetçi (bağımsız) RED: bu kez bulgular gerçek sızıntı yolları.**
> Denetçi önce doğruladı: `*Authenticator` / `Authenticator` / `**Authenticator` doğrudan 34–40
> yolda temiz, `*Authenticator` dışa kapalı alanda 12 fiilde temiz, kapalı küme mutantlarının
> çoğu kırmızı, 5. turun N1–N6'sı kapalı, 14 ağır mutasyon kırmızı, zamanlama testleri 50/20/40,
> staticcheck son değişiklikten sonra rc=0. **Bloklayıcılar:** B1 — `*[]byte` dolaylaması
> `%s`-ailesinde hiçbir şey korumuyordu (fmt'nin `badVerb`'ü, işaretçinin kabul etmediği bir
> fiilde işaretçiyi derinlik 0'da bir kez açar; dizi/dilim/struct/map'i gösteren işaretçiyi açar,
> string'i göstereni açmaz): `*authKeys` dört anahtarı, `Secret`/`Pending` düz TOTP sırrını,
> `db.SealedSecret`/`db.OperatorAccount` zarfı `%s %q %e %f %t %c %U`'da bastı; B2 —
> `Config`'in "açık kalan tek yolu" tek değildi (`%p` ve `%w` fmt'nin `erroring` bayrağıyla
> yöntemleri kapatır; dışa kapalı alan her fiilde sızıyordu) ve "OP-7'nin API'sini değiştirir"
> gerçek bir maliyet değildi; B3 — kapalı kümenin "bayat giriş kırmızı" iddiası `db.*` için
> boştu (istisnalar kökken bir giriş kendini ulaşılabilir kılıyordu). Orkestratörün kararı: sırlar
> yalnız `*string` arkasında (repo emsali, sınıfı kıran kural); `Config` → `Key`; kapalı kural
> pini; render matrisi; kökler yalnız paketin kendisi.
>
> **Ürün değişikliği** (ADR 0020 §2'ye tarihli not; 6. turun notu üstü çizilerek düzeltildi):
>
> - `Secret` → `struct{ b *string }`, `db.SealedSecret` → `struct{ v *string }`. `Secret.Zero`
>   artık baytları SİLEMEZ (Go dizgesi değişmez) — değeri unutturur (değer ve kopyaları bir daha
>   bir şey açmaz). Kayıp değil, ölçülerek: ekranın `Base32`/`URI` dizgeleri ve render edilen
>   sayfa zaten silinmiyordu; oturum açılışında ve enrollment'ta açılan sır yerel `[]byte`'tır ve
>   `sun.Zero` ile silinir (değişmedi). `**[]byte` de `badVerb`'e karşı kapatırdı (denetçinin
>   ölçümü) ve silmeyi korurdu; tek kural için `*string` seçildi.
> - Yeni `operatorauth.Key` (`key.go`): `struct{ v *string }`, `NewKey([]byte)` (kopya alır),
>   beş yöntem (`operatorauth.Key(redacted)`), **dışa açık erişimci yok** — OP-7'nin eşitsizlik
>   reddi `internal/config`'te ham değerlerde, `NewKey`'den önce koşar (`config.keySeparation`
>   emsali; OP-4 bloğu, OP-7 listesi). `Config.TOTPKEK`/`Config.TokenHMACKey` artık `Key`.
> - `Authenticator`'ın anahtarları — iki yol: (i) `keys **authKeys` (denetçi kopyasında ölçtü:
>   kapatır); (ii) `authKeys`'in dört alanı `Key` (bu ağaçta ölçüldü: matriste 0 sızıntı).
>   **(ii) seçildi:** tek kural (her sır kendi redakte eden tipinde), `Config` ile aynı tip,
>   anahtar hangi yapıya kopyalanırsa kopyalansın korunur; (i) yalnız o işaretçinin arkasında
>   korur ve her kullanım yerinde çift dereferans ister. `keys` artık değer (`authKeys`).
> - Hash/HMAC/`Seal`/`Open`/bcrypt'in istediği `[]byte` yalnız kullanım anında,
>   `Key.bytes()`'ın kopyası olarak üretilir; silinmez — anahtar süreç boyunca zaten bellekte.
> - API: yalnız `Config`'in iki alanının tipi değişti; paket dışında kullanıcı yok (OP-7
>   yazılmadı; denetçinin saydığı 15 kullanım yeri paketin kendi testlerinde, uyarlandı).
>
> **Ölçülen matris** (`TestLeak_NoSecretOnAnyPrintingPath`, `render`; sözleşmenin numaralı
> listesi testte): kapalı kümenin her specimen'i (13 tip, `Key` dahil) × fmt'nin 22 fiili
> (`%p`, `%w` dahil) × 6 biçim S1–S6 (değerin kendisi, dışa açık `any` alanı, dışa kapalı `any`
> alanı, dışa kapalı alanda işaret edilen değer, dilim elemanı, map değeri) × `Sprintf` ve
> `Errorf` + F1–F5 (slog text ×2, slog JSON, `json.Marshal` ×2) — 3497 render, **0 sızıntı**;
> yöntemlere ulaşılan her yolda yer tutucu. Aranan biçimler sırrın kendisinden türer
> (`byteForms`: ham, hex ×2, ondalık liste, `%#v` listesi, `%q` kaçışı, JSON dizesi — HTML
> kaçışlı ve kaçışsız —, base64; `verbForms`: fiilin baytlara ve metne uygulanmışı). **Pozitif
> kontrol her yol için:** her specimen'in ilk sırrı korumasız — metin ve bayt olarak — matrisin
> her yolunda bulunmak zorunda; yalnız fmt'nin YALNIZ ADRES bastığı yollar (`%p` × işaretçi,
> dilim, map) muaf. Kontrolün bulduğu iki eksik: `%c`/`%U`/`%e`… altında `[]byte` hiçbir
> sabit biçimle eşleşmiyordu (`verbForms` bu yüzden var) ve slog'un JSON handler'ı HTML kaçışı
> yapmıyor (rastgele bir sırda `<`, `>` ya da `&` varken; 200 tekrarlı koşuda yeşil).
> `knownLeaks` kaldırıldı — boştu.
>
> **Kapalı kural pini** (`TestExportedTypes_CarryNoPlainStringField`, üç kural): (1) yürünen
> her alan — bu paketin/db'nin struct tipindeki alanlar DAHİL — `allowedFields`'ta ve tersi;
> (2) hiçbir alan metni ya da baytı açıkta tutmaz (`string`, `[]byte`, `[N]byte`, `*[]byte`,
> `*[N]byte`; *8. turda:* `[]rune`, `[N]rune`, `*[]rune` de — liste `isBytesOrText`'in
> kendisidir; arayüz tipli bir alanın ÇALIŞMA ZAMANI içeriği bu kuralın dışında, sayılı
> sınır); (3) tek alanlı redakte eden bir tip değerini `badVerb`'ün açmadığı bir
> işaretçinin arkasında tutar (dizi/dilim/struct/map dışı bir şeyi gösteren işaretçi).
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B1 | `*[]byte` / `*authKeys` `badVerb`'de açılıyordu | `*string` + `Key`; kural (2)–(3); matris | M7-Secret-bytes: *"Secret leaks on Sprintf %s · S3 an unexported any field"* (+ `Zero` testi) · M7-SealedSecret-bytes: *"db.OperatorAccount leaks on Sprintf %s · S3 …"* · M7-Key-bytes: *"Key leaks on Sprintf %s · S3 …"* · M7-SessionToken-bytes; beşinde de alan kuralları ayrıca kırmızı |
> | B2 | `Config`'in yolları `%p`, `%w`, her fiilde dışa kapalı alan | `Config` → `Key`; matris `%p`/`%w`/S3/S4 dahil | M7-Key-plainBytes (`type Key []byte` + beş yöntem — denetçinin "hiçbir şey kapatmaz" ölçümü): *"Key leaks on Sprintf %w · S1 the value itself"* · M7-Key-Format, M7-Key-LogValue (*"Key: slog resolves it to a Any"*) |
> | B3 | Ulaşılamayan `db.*` istisnası yeşildi | Kökler yalnız paketin kendi dışa açık bildirimleri (`apiTypes`) | M7-B3-staleTag, M7-B3-staleConn: *"the exception db.ResolvedTag / db.OperatorConn is not reachable from this package's exported API"* |
> | N1 | `db.*` taşıyan dışa açık fonksiyon/değişken, dışa kapalı tip döndüren yöntem, fonksiyon içi tip | `apiTypes`: iki paketin DAR tip denetimi (`go/types`; başka her import boş bir paket, hataları yok sayılır — yalnız iki paketin adlı tipleri okunur; `-race` altında 55 ms, `go/importer`'ın tam kaynak içe aktarımı ~15 s — ölçüldü); ~~dışa açık her bildirimden ulaşılan her adlı tip~~ *(8. turda yanlışlandı: başka bir paketin generic'inin TİP ARGÜMANI görülmüyordu — o tip geçersiz çözülür ve go/types argümanları değerlendirmez; 8. tur alt bölümü)*; fonksiyon içi tip reddi. **Sayılı sınır:** çalışma zamanında başka bir paketin tipiyle doldurulan `any` kümede değil | M7-N1-func / -var: *"db.ResolvedTag / db.ResolvedAdmin is reachable … and is neither a specimen nor a named exception"* · M7-N1-method: *"authKeys is reachable …"* · M7-N1-local: *"a function-local type t (operatorauth.go:206:7) …"* |
> | N2 | Struct tipindeki alanlar adlandırılmıyordu | Artık adlandırılır (kural 1) | M7-N2-cfg: *"Authenticator.cfg is a field nothing authorised"* · M7-N2-lastCode · M7-rule-plainString: *"Challenge.raw holds text or bytes in the open (string)"* |
> | N3 | `TestRedaction_EveryMethodOnEveryType` 7'nin 5'ini kapsıyordu | Tablo kaynağa pinli: `Format` bildiren her tip (bu turda 8: `Key`, `Config`, `Authenticator` eklendi) | M7-N3-table: *"Key declares Format and is not in the table"* · M7-Key-Format: *"the table names Key, which declares no Format"* |
> | N4 | Çerez kolları R1–R4 render'larla çakışıyordu | K1–K4 (test başlığı, arm adları, kart) | — |
> | N5 | "the two unexported-field paths" | Cümle kalktı: yollar artık numaralı matris | — |
> | N6 | Sıfır değerli `Authenticator` | İşlem yok (sıfır değer kullanılamaz) | — |
>
> **7. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** bu turun 17'si
> — **17/17 kırmızı** (M7-Key-LogValue'nun ilk biçimi derlenmiyordu — kullanılmayan import —,
> düzeltilmiş biçimi kırmızı); 6. turun kendi listesi 17 — **13 kırmızı + 4 derleme kırmızısı**
> (B1-MarshalText ×2, B1-StringAuth, B1-GoStringConfig: `TestRedaction_EveryMethodOnEveryType`'ın
> `printer` arayüzü bu yöntemleri artık `Authenticator` ve `Config` üzerinde de ister, paketin
> testleri derlenmez); 6. turun üçü eskidi (B1-lastCode → M7-N2-lastCode, B1-keysByValue →
> M7-Key-bytes, B1-knownLeakStale → `knownLeaks` yok); bu kartın 75'i — **75/75**; 5. turun 8'i
> + D6 — **9/9**; 1. denetçinin 25'i — **25/25**; 2. denetçinin 62'si — **62/62**.
> **Yeni yeşiller ve sebepleri, ölçülerek:** 4. denetçinin BA6 ve D2'si, 3. denetçinin A-N1 ve
> G-N1kek'i, 5. denetçinin B-D2 ve C-N1'i — altısı da `New`'un hata metnine `cfg.TOTPKEK` ya da
> `cfg.TokenHMACKey`'i `%x`/`%#v`/`%v` ile koyar; bu alanlar artık `Key` ve `Format` yer tutucu
> basar, yani **mutant artık sızdırmıyor** (eşdeğer mutant). Ölçüldü (scratchpad kopyası): BA6+D2
> birleşik mutantında `New`'un hatası `… got 31 (operatorauth.Key(redacted))
> operatorauth.Key(redacted)`. Altısının **bilerek çıkaran** biçimi (`cfg.TOTPKEK.bytes()`, dışa
> kapalı erişimciyle) — **6/6 kırmızı**, ör. *"error 73 carries member 2 (G11 TOTP KEK)"*. Kalan
> yeşiller öncekiyle aynı ve beklenen: 4. denetçinin D3, F1, BB2'si; 5. denetçinin B-D3, BB2,
> K-Hcontent2'si ve E-c ×2'si; 5. denetçinin derlenmeyen K-G17empty'si (ikizi kırmızı). Toplam:
> 4. denetçinin 37'si (+ D6) — 32 kırmızı; 3. denetçinin 55'i — 53 kırmızı; 5. denetçinin 66'sı —
> 58 kırmızı, 7 yeşil, 1 derlenmeyen.
>
> Tam koşu (`.env`, `-race -count=1 ./...`, 2026-09-30 22:43 UTC, yük 2,8–5,9): **bu koşuda** tek
> kırmızı T72 (`cmd/rotatekek`); `internal/operatorauth` 171,6 sn, `internal/db` 38,0 sn, duvar 287
> sn. Tam koşudan sonra tek değişiklik test tarafında: `TestNew_RefusesWhatItCannotUse`'a sıfır
> `Key` vakası (kapsam `Key.size`'ın sıfır dalını ölçmüyordu); ardından paket `-race` ile
> yeniden koşuldu (yeşil, 140,0 sn), build, vet, `gofmt -s`, redline, `TestEveryNamedTestExists`
> ve staticcheck (son değişiklikten sonra, rc=0). Kapsam yukarıda (doğrulama bloğu md. 2).
>
> **8. tur (2026-09-30) — 7. denetçi (bağımsız) RED: ürün kodunda sızıntı yok** (bağımsız
> sondası: 15 specimen × fiiller × 28 matris dışı kalıp — `**T`, derinlik 2–3, reflect,
> template, gob/xml, `errors.Join`, panic — 47 925 render, 0 isabet). Doğruladıkları: 6 yeni
> yeşilin açıklaması, `Zero` semantiği, sıfır `Key` reddi, AAD ve 48 baytlık zarf, ağır
> mutasyonlar, zamanlama 50/20/60 ve matris ×200. **Tek bloklayıcı, adıyla sayılmamış bir
> kaçış:** `apiTypes` başka bir paketin generic tipinin TİP ARGÜMANI olarak ulaşılan tipi
> görmüyordu (`func Admins() iter.Seq[db.ResolvedAdmin]`, `func Latest()
> *atomic.Pointer[db.ResolvedTag]` yeşil; aynı erişim func tipiyle kırmızı).
>
> **B1 — ölçerek seçildi: tip argümanları yürütülür** (ucuzdu; sayılı sınıra yazmak
> gerekmedi). *(9. turda yanlışlandı: sözdizimiyle okuma takma adı ve çıkarımla tipi gelen
> değişkeni kaçırıyordu — 8. denetçinin 10 mutantı; `names` kaldırıldı, tipler kesin
> yükleniyor — 9. tur alt bölümü.)* Mekanizma, ölçüldü: dar denetimde başka paketin tipi GEÇERSİZ çözülür ve
> go/types geçersiz bir generic'in tip argümanlarını HİÇ değerlendirmez (`info.Types`/
> `info.Instances` de boş kalır) — bu yüzden argümanlar sözdizimiyle okunur: `apiTypes`'ın
> `names`'i, API bildirimlerinin METNİNDE adı geçen her tipi (paketin kapsamındaki bir ad ya da
> iki paketten biriyle nitelenmiş bir ad) ziyaret eder. Uygulandığı yerler: dışa açık
> fonksiyonların imzaları, dışa açık değişken/sabit bildirimleri (tip ve ilk değer), yürüyüşün
> ulaştığı her adlı tipin `TypeSpec`'i (struct'ta yalnız dışa açık ve gömülü alanlar) ve dışa
> açık yöntemlerinin `FuncDecl`'ları. **Sayılı sınır, adıyla:** başka bir paketin BİLDİRDİĞİ bir
> tipin içinden — bu paketin metninde yazılmadan — ulaşılan tip izlenmez (öteki paketler
> yüklenmez); `names` sözdizimiyle çözdüğü için bir tip adını gölgeleyen yerel bir DEĞİŞKEN o
> tipi ulaşılabilir saydırır (fail-closed yön). İddia bu ölçüye daraltıldı: test yorumları,
> md. 18, OP-8 devri, 7. tur bölümünün N1 satırı (üstü çizilerek).
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B1 | Generic tip argümanı kaçışı | Yukarıda (`names`) | M8-B1-iterSeq (denetçinin `func Admins() iter.Seq[db.ResolvedAdmin]`'i): *"db.ResolvedAdmin is reachable from this package's exported API and is neither a specimen nor a named exception"* · M8-B1-atomicPtr (`*atomic.Pointer[db.ResolvedTag]`): *"db.ResolvedTag is reachable …"* · aynı tip argümanı dışa açık bir YÖNTEMDE (M8-B1-method), dışa açık bir DEĞİŞKENDE (M8-B1-var) ve dışa açık bir STRUCT ALANINDA (M8-B1-field) — kırmızı · KONTROL M8-B1-control-func (func tipiyle aynı erişim) — kırmızı |
> | N1 | Kural (2)'den `[]rune` ve `any` alanındaki `[]byte` | `isBytesOrText`'e `[]rune`, `[N]rune`, `*[]rune` (int32 elemanlı dilim/dizi); arayüz tipli alanın çalışma zamanı içeriği sayılı sınır (test yorumu, md. 18); kartın düzyazısı listeyle eşleşti | M8-N1-runes ve M8-N1-runesOnly (`allowedFields`'ta adlandırılmış — kural (2) tek başına): *"Challenge.r holds text or bytes in the open ([]int32)"* |
> | N2 | OP-7 devrinde çelişki (`config` `operatorauth`'u import edemez) | Orkestratörün kararıyla yeniden yazıldı (OP-4 bloğu, OP-7 listesi; ADR 0020 notu; `key.go` yorumu): anahtarlar `config.Config`'te ham `[]byte`, ayrılık reddi orada; `Key`'e dönüşüm `cmd/tappa` wiring'inde `NewKey` ile; 6. tur kuralı `operatorauth` tarafına daraltıldı (üstü çizilerek) | — |
> | N3 | `Store` yuvası ve bütçe haritaları | Ölçüldü (scratchpad): dışa kapalı alandaki bir `Authenticator` DEĞERİ `%s`/`%q` altında `store`'un içeriğini bir kez açar — düz alanda DSN basıldı (değer store: `%v %s %q %d %+v`; işaretçi store: `%s %q`), `*string` alanda basılmadı *(9. tur: fiil listeleri ölçümün alt kümesiydi — 8. denetçi düz alanlı değer store'unda 28/28, işaretçi store'da 14 fiil ölçtü; md. 18 ve OP-7 devri artık fiil listesi yazmıyor: "düz alan yok")* — ve bütçe haritalarının anahtarlarını basar (`badVerb` `*budget`'i açar). `limits *limits` ile ölçüldü: adres hiçbir fiilde görünmedi. ~~**Ürün değişikliği YAPILMADI** (kararı sonraya).~~ → 8b satırı: yapıldı. Sayılı sınır (md. 18, ADR notu) + OP-7 devrine tek cümle: `db.OperatorDB` DSN'i ya da parolayı düz alanda tutmaz, havuz yalnız işaretçiyle | — |
> | N4 | `NewKey`'in kopyası pinsiz | `TestKey_CopiesInAndOut` (çağıranın dilimini silmek ve `bytes()`'ın kopyasını silmek `Key`'i değiştirmez) | M8-N4-unsafe (`unsafe.String(unsafe.SliceData(b), len(b))`): *"wiping the caller's slice changed the key"* (`TestKey_CopiesInAndOut`) |
> | N5 | Tip parametresi "function-local type" diye raporlanıyordu | `*types.TypeParam` atlanır; gerçek fonksiyon içi tip reddi sürer | KONTROL M8-N5-typeparam (dışa açık `func Pick[T any](v T) T`) — **yeşil, beklenen** · M8-N5-revert-skip (atlamayı silip aynı fonksiyon): *"a function-local type T (operatorauth.go:205:11) …"* — kırmızı; gerçek fonksiyon içi tip (M7-N1-local) hâlâ kırmızı |
> | N6 | `sec.reveal()` kopyası silinmiyordu | `BeginEnrollment`: `pt := sec.reveal(); defer sun.Zero(pt)`; `Secret.Base32` de kopyasını siler. Öteki `reveal()`'lar dizge döner (token, challenge — kopya yok); `Key.bytes()` kopyaları silinmez — süreç ömrü anahtarı, sayılı sınır. Silme gözlenebilir değil — pinsiz, adıyla | — |
> | 8b | N3 kararı (orkestratör): bütçe haritaları işaretçinin arkasına | `Authenticator.limits` → `*limits` (`newLimits` `*limits` döner) — tek ürün değişikliği. Pin: sızıntı matrisinin `Authenticator` specimen'i `AllowRequest("192.0.2.123")` ile doldurulmuş bütçe haritası taşır ve adres specimen'in sırlarından biridir. `Authenticator`'ın öteki alanları sayıldı (md. 18): işaretçi arkasına alınacak başka adres/kimlik bilgisi tutan alan yok | M8b-limits-byValue (`limits limits` geri dönüşü): *"Authenticator leaks on Sprintf %s · S4 the pointee, in an unexported any field (11 characters …)"* — `%q` ve `Errorf` de; ağır örnekler hâlâ kırmızı: D4, X8, X2, M1, H1, L1 |
>
> **8. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** bu turun 11'i
> — **10 kırmızı, 1 beklenen yeşil** (M8-N5-typeparam kontrolü); 7. turun 17'si + `Key.LogValue`'nun
> derlenen biçimi + altı çıkarma biçimi — **24/24 kırmızı** (7. turun eski, derlenmeyen
> `LogValue` biçimi listede kaldı); 6. turun 17'si — **13 kırmızı + 4 derleme kırmızısı** (7.
> turdaki gibi); 5. turun 8'i + D6 — **9/9**; bu kartın 75'i — **75/75** (E2'nin metni
> `BeginEnrollment`'ın yeni `pt`'sine uyarlandı); 1. denetçinin 25'i — **25/25**; 2. denetçinin
> 62'si — **62/62** (M1 dahil); 4. denetçinin 37'si — **32 kırmızı**; 3. denetçinin 55'i — **53
> kırmızı**; 5. denetçinin 64'ü — **58 kırmızı**. Yeşiller 7. turla birebir aynı ve sebepleri
> aynı: eşdeğer mutantlar (BA6, D2, A-N1, G-N1kek, B-D2, C-N1 — `Key` yer tutucu basar; `.bytes()`
> biçimleri kırmızı) ve beklenenler (D3, F1, BB2, B-D3, K-Hcontent2). İstenen örnekler
> kırmızı: D4, D5, X8, X2, M1, Y20, H1, R3, L1, E2 ve tip geri dönüşleri (M7-Secret-bytes,
> M7-Key-bytes, M7-SealedSecret-bytes, M7-Key-plainBytes).
>
> Tam koşu (`.env`, `-race -count=1 ./...`, 2026-10-01 00:50 UTC, yük 3,2–4,2): **bu koşuda** tek
> kırmızı T72 (`cmd/rotatekek`); `internal/operatorauth` 168,9 sn, `internal/db` 40,7 sn, duvar 278
> sn. Kapı zinciri baştan, staticcheck SON değişiklikten sonra (rc=0); tam koşudan sonra kod
> değişmedi. Kapsam yukarıda (doğrulama bloğu md. 2). **8b'den sonra** (kod değişti): kapı
> zinciri baştan (staticcheck son değişiklikten sonra rc=0) ve tam koşu yeniden (2026-10-01
> 01:00 UTC): bu koşuda tek kırmızı T72; `internal/operatorauth` `-race` ile 168,7 sn, yeşil.
>
> **9. tur (2026-10-01) — 8. denetçi (bağımsız) RED.** Doğruladıkları: 7. turun bulgularının hepsi
> doğru test ve doğru sebeple kapalı; bağımsız ürün sondası 2 914 + 14 756 render, 0 isabet
> (pozitif kontroller — `limits limits`, `Key *[]byte` — isabet verdi); N2 devri uygulanabilir
> (import döngüsü kanıtlandı, `cmd/tappa`'da prob derlendi); 16 ağır mutasyon kırmızı; zamanlama
> temiz; ADR uyumu tam. **Bloklayıcı B-1:** `names` iki biçimde başka paketin generic örneğini
> kaçırıyordu — tip TAKMA ADI (`Alias` kolu yalnız `Unalias`'a gidiyordu, dar denetimde
> geçersiz) ve tipi ÇIKARIMLA gelen dışa açık değişken (dışa kapalı bir fonksiyonun, değişkenin
> ya da yöntemin imzasından); denetçinin 10 derlenen mutantı iki ExportedTypes testini yeşil
> bıraktı. Orkestratörün kararı: sözdizimini kovalama; iki yoldan birini ÖLÇEREK seç.
>
> **İki yolun ölçümü ve karar:**
>
> - **(b) KESİN TİPLER — SEÇİLDİ.** `go list -export -deps -json` bağımlılıkların export verisini
>   (derleyicinin yazdığı) adlandırır; `go/importer.ForCompiler(fset, "gc", lookup)` okur; test
>   `-race` ile derlendiyse `go list` de `-race` alır (`race_on_test.go`/`race_off_test.go`
>   yapı etiketi), böylece testi derleyen önbellek girdileri kullanılır. Stdlib yalnız, yeni
>   bağımlılık yok. Süre, ölçüldü (`apiTypes` bütünü — `go list` + denetim + yürüyüş — bu
>   paketin kendi testinde): **sıcak önbellek 0,23 sn, `-race` 0,35–0,39 sn; soğuk önbellek**
>   (yeni bir `GOCACHE`, test ikilisi sıfırdan derlenir) **0,58 sn, `-race` 0,95 sn** — kural
>   ≤ ~5 sn. Testi derleyen araç zinciriyle `go`'nun sürümü farklıysa test bunu adıyla reddeder
>   (`go env GOVERSION` ≠ `runtime.Version()`); cmd/go araç zinciri değiştirdiğinde doğru `go`'yu
>   PATH'in başına koyar (ölçüldü: `GOTOOLCHAIN=go1.26.7` altında alt süreç 1.26.7).
>   Yürüyüş: dışa açık her bildirimin ÇÖZÜLMÜŞ tipi (değişken ve sabitler dahil), imzalar, dışa
>   açık alanlar, dışa açık yöntemler, arayüzler, `types.Alias` (`Unalias` + takma adın kendi
>   tip argümanları), her generic örneğin `TypeArgs()`'ı — hangi paketin olursa olsun. ~~Başka
>   bir paketin tipi bu iki paketin tipini YALNIZ tip argümanı olarak taşıyabilir (…), o kapı
>   yürünür — 8. turun "başka paketin içinden" sınırı kalktı.~~ *(10. turda yanlışlandı:
>   öncül yanlıştı — `operatorauth` → `internal/sun` → `internal/db`; `sun.Result` bir
>   `db.ResolvedTag` taşır; 10. tur alt bölümü.)* **Kalan sayılı sınır:** çalışma zamanında
>   doldurulan `any` (arayüz) ve reflection.
> - **(fail-closed) GÖREMEDİĞİNİ YASAKLA — ölçüldü, seçilmedi.** Sayım: `internal/operatorauth`
>   ve `internal/db`'de tip takma adı 0, nokta-import 0; açık tipi olmayan dışa açık
>   `var`/`const`: ~~13 sabit~~ *(10. turda AST ile yeniden sayıldı: `operatorauth`'ta 9 sabit,
>   `internal/db`'de 1 sabit ve 2 sentinel değişken)* (temel literal — izinli biçim) ve **8
>   sentinel hata** (`ErrRefused …
>   = errors.New(…)` — çağrı ifadesi, izinli üç biçimin dışında): kural ya ürünü değiştirmeyi
>   (`var ErrRefused error = …`) ya da adıyla bir istisnayı gerektirirdi. (b) bu bedeli
>   istemiyor ve iddiayı "tipler neyse o" yapıyor.
>
> `names`, `narrowImporter` ve `ownPackage` kaldırıldı. Yazılı iddia — test yorumu (`exactImports`,
> `apiTypes`, kapalı küme testinin başlığı), md. 18, OP-8 devri (seçilen yol ve nedeni) ve 8. tur
> B1 bloğu (üstü çizilerek) — mekanizmayla birebir.
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B-1 | Takma ad ve çıkarımla tipi gelen değişken kaçışı | Kesin tipler (yukarıda) | Denetçinin 10'u: C2a, C2b, C2c, C2d, C2e, C8b (db tarafı takma ad), C6b, C6c, C6d, C6g — **10/10 kırmızı**, ör. *"db.ResolvedAdmin is reachable from this package's exported API and is neither a specimen nor a named exception"*; nokta-import (`. "iter"`, `func F5() Seq[db.ResolvedAdmin]` — `New` çakışmasız) **kırmızı**; denetçinin `. "…/db"` biçimi derlenmiyor (`New` çakışması — beklenen); daha önce kırmızı olanlar kırmızı: C1, C2f, C3, C4, C4b, C5 (`dbx`), C6a, C6e, C6f, C11, C8a, Cx ×4, B-* (15'in 14'ü; B-limits-byValue-unfilled pinin mekanizmasını gösteren beklenen yeşil), M8-B1-* ×6. **Mekanizma kontrolü:** `TypeArgs` yürüyüşü silinince C2a ve iterSeq YEŞİL (iki kontrol — satır yük taşıyor) |
> | N-1 | Kural (2) `[N]string`'i görmüyordu | `isBytesOrText`: dize dizisi, dize dilimi ve onlara işaretçi; `**[]byte` adıyla dışarıda (fmt adres basar); dizi eleman sınırı adıyla | M9-N1-stringArray (`r [4]string`, `allowedFields`'ta adlı): *"Authenticator.r holds text or bytes in the open ([4]string)"* · `[]string`, `*[4]string` — kırmızı |
> | N-2 | DSN sınırının fiil listeleri ölçümün alt kümesiydi | md. 18, OP-7 devri md. 4, 8. tur N3 satırı, ADR 0020 notu: fiil listesi yok, "`store`'un düz alanı `Authenticator`'ın tutuluşundan bağımsız basılabilir; kural: düz alan yok" (8. denetçinin 28 fiillik ölçümü atıfla) | — |
> | N-3 | Bütçe yarısı şimdiki zamanla duruyordu | ADR 0020 ve md. 18'de üstü çizildi (8b'den beri kapalı; 8. denetçi 0 isabet) | — |
>
> **9. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı):** 8.
> denetçinin 41'i (probe dosyası içeriği repoya YENİ dosya olarak konmadı — `key.go`'nun sonuna
> eklenip geri yazıldı) — **39 kırmızı, 1 beklenen yeşil** (B-limits-byValue-unfilled: pinin
> mekanizması), **1 beklenen derleme kırmızısı** (C5b); bu turun 6'sı (nokta-import, üç dize
> biçimi, iki mekanizma kontrolü) — 4 kırmızı + 2 beklenen yeşil kontrol; 8. turun 11'i — 10
> kırmızı + N5 kontrolü (beklenen yeşil); ağır örnekler 14/14 kırmızı: D4, D5, X8, X2, M1, H1,
> L1, R3, tip geri dönüşleri (Secret, SealedSecret, Key, SessionToken `*[]byte`; `type Key
> []byte`) ve `limits limits`.
>
> Kapı zinciri baştan, staticcheck SON değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race
> -count=1 ./...`, 2026-10-01 01:54 UTC, yük 2,9–4,5): **bu koşuda** tek kırmızı T72
> (`cmd/rotatekek`); `internal/operatorauth` 171,8 sn, `internal/db` 126,2 sn, duvar 280 sn.
> Kapsam: `internal/operatorauth` %95,6 (44 test), `internal/sun` %97,4.
>
> **10. tur (2026-10-01) — 9. denetçi (bağımsız) RED.** Doğruladıkları: CI uyumu (Linux
> konteyner `golang:1.26`, ağ yok, `GOPROXY=off`, `-mod=readonly`, salt-okunur ve soğuk
> önbellek, `-race`: PASS, C2a kırmızı); dört bozuk ortamda (sürüm uyuşmazlığı, PATH'te `go`
> yok, `-mod=vendor`, başka cwd) FAIL, SKIP değil; `-count=10 -parallel 8 -race` temiz; 8.
> denetçinin 12 mutantı ve kendi 18 yeni biçimi kırmızı; ürün sondası 1 796 render, 0 isabet;
> 14 ağır + 5 yeni ağır mutant kırmızı. **Bloklayıcılar:** B-1 — 9. turun "başka paketin tipi
> bu iki paketin tipini yalnız tip argümanı olarak taşıyabilir" öncülü yanlıştı:
> `operatorauth` → `internal/sun` → `internal/db`, `sun.Result.Tag` bir `db.ResolvedTag` (açıkta
> `AESKeyRef []byte`); yabancı `Named`'de yalnız `TypeArgs` yürünüyordu, `func R() sun.Result`,
> `func V() *sun.Verifier`, `var F = sun.NewVerifier` yeşildi. B-2 — tip parametresi kısıtı,
> gömülü arayüz tipleri ve union terimleri yürünmüyordu. **Orkestratörün kararı: kapanışı
> öncüle değil YAPIYA bağla.**
>
> **Yapılan (yalnız test ve belge; ürünün tek değişikliği `password.go`'da bir yorum):**
>
> - **Tükenmiş anahtar.** `apiTypes`'ın `visit`'i go/types'ın her tür türünü ADIYLA ele alır —
>   Basic, Pointer, Array, Slice, Map (anahtar ve değer), Chan, Struct (HER alan, dışa açık ya da
>   kapalı — fmt kapalı alanları da basar), Tuple, Signature (alıcı, alıcı ve fonksiyon tip
>   parametreleri, parametreler, sonuçlar), Named (tip argümanları, tip parametreleri, DIŞA AÇIK
>   yöntemlerin imzaları, alt tip), Alias (argümanlar, tip parametreleri, `Unalias`),
>   Interface (açık yöntemler — dışa kapalılar dahil — ve gömülü tipler), Union (terimler),
>   TypeParam (kısıt); `default:` → `t.Fatalf("unhandled go/types kind %T")`. Hangi paketin
>   olursa olsun her tip yürünür; KAYIT yalnız `operatorauth` ve `internal/db` adlarıyla
>   *(11. turda modülün her paketine genişledi)*.
>   Öncül cümlesi her yerden kalktı (test yorumu, md. 18, 9. tur alt bloğu — üstü çizilerek —,
>   OP-8 devri, ADR 0020); yerine: "yürüyüş, go/types'ın tip grafiğini tükenmiş bir anahtarla
>   tam dolaşır; tanınmayan tür = kırmızı". **Kalan sayılı sınır:** yalnız çalışma zamanında
>   doldurulan `any` ve reflection; ve, adıyla: bir adlı tipin DIŞA KAPALI yöntemleri izlenmez
>   (paket dışından çağrılamaz, yazdırılacak değer tutmaz). *(11. tur: bu iki sınır ve öteki
>   hepsi md. 18'in tek listesinde, S1 ve S2.)*
> - **Ölçüm — bugünkü graf:** ~~1 953 tip (Alias 1, Array 4, Basic 18, Chan 2, Interface 18,
>   Map 11, Named 112, Pointer 354, Signature 492, Slice 207, Struct 64, Tuple 670)~~ *(11.
>   tur, N-2: sayı platforma bağlı — Linux go1.26.8'de 1 935 (denetçi); kayıt sayıyı tutmaz,
>   test her koşuda `t.Logf` ile basar)*; Union ve TypeParam bugünkü grafta YOK. **Küme bu turda dört tip büyüdü** — hepsi bu paketin dışa
>   kapalı tipleri, `Authenticator`'ın dışa kapalı alanlarından: `authKeys`, `limits`, `budget`,
>   `budgetWindow`; gerekçeli istisna oldular (yalnız bir `Authenticator`'ın parçası olarak
>   basılırlar — o bir specimen, ~~matris onu kapsar~~ *(11. turda yanlışlandı: specimen
>   türetilmiş challenge anahtarını ve `dummyDigest`'i aramıyordu; gerekçeler artık yalnız
>   mekanik olarak denetlenen iddialar — 11. tur bloğu)*; reflect tipleri `Authenticator`'ın
>   alanlarından okunur, eksik alan kırmızı). **Yeni db tipi YOK:** `db.ResolvedTag` (ve onun
>   açıktaki `AESKeyRef []byte`'ı) OP-6'nın API'sinden bugün ULAŞILAMIYOR — ölçüm: `internal/sun`
>   import ediliyor ama dışa açık hiçbir bildirim bir `sun` tipi vermiyor; `sun.Result` ya da
>   `*sun.Verifier`'ı veren mutant kırmızı. Kümenin bugünkü üyeleri: specimen 13 —
>   `SessionToken`, `Challenge`, `Secret`, `EnrollmentToken`, `PendingBlob`, `Issued`,
>   `Pending`, `Key`, `Authenticator`, `Config`, `db.OperatorAccount`, `db.SealedSecret`,
>   `db.PasswordHash`; istisna 8 — `Identity`, `Store`, `db.OperatorSession`,
>   `db.OperatorAuthEvent` ve bu turun dördü. Süre, `apiTypes` bütünü: sıcak 0,19–0,32 sn
>   (`-race` 0,37–0,53 sn), soğuk 0,43 sn (`-race` 0,83 sn).
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B-1 | Yabancı `Named`'in alanı/yöntemi yürünmüyordu (öncül yanlış) | Tükenmiş anahtar, yabancı tip tam | A9-sun-Result, A9-sun-Verifier, A9-sun-NewVerifier-funcvalue, A9-sun-field-in-exception — *"db.ResolvedTag is reachable from this package's exported API and is neither a specimen nor a named exception"*; A9-sun-Preview-method **yeşil, doğru**: `sun.Preview`'da hiçbir db tipi yok (alanları bool, uuid, string, *uuid; yöntemi yok) — eşdeğer mutant |
> | B-2 | Kısıt, gömülü arayüz, union yürünmüyordu | `TypeParam`, `Interface` gömülüleri, `Union` | A9-constraint-inline, -named, -union, -generic-type — *"db.ResolvedAdmin is reachable …"* |
> | Anahtarın pozitif kontrolü | — | `default:` → `t.Fatalf` | M10-ctl-noBasic (bugünkü grafta 18 Basic): kırmızı, `default` ateşler. M10-ctl-noUnion ve M10-ctl-noTypeParam yalnız başına **yeşil — adıyla eşdeğer**, çünkü bugünkü grafta union ve tip parametresi yok; union ya da kısıt mutantıyla birlikte KIRMIZI (`default` ateşler) |
> | N-1 | Adsız struct'ın dışa kapalı alanı; kural (2) adsız struct'a uygulanmıyordu | Yürüyüş her alanı; alan testi adsız struct'ı da yürür, alanlarını yolu altında adlandırır (`Authenticator.r.s`) | A9-anon-struct-unexported-field (`func Snap() struct{ a db.ResolvedAdmin }`): kırmızı; M10-N1-anonString (`r struct{ s string }`, iki yol da `allowedFields`'ta): *"Authenticator.r.s holds text or bytes in the open (string)"*. Kontrol: struct yürüyüşü dışa açık alanlarla sınırlanınca Snap'in db tipi yakalanmadı (test yalnız iki iç istisnanın bayatlığıyla kırmızı) |
> | N-2 | Map eleman cümlesi | `isBytesOrText` map'in DEĞERİNİ okur (anahtarı değil — `budget.windows` hız anahtarlarıyla, adıyla) | M10-N2-mapString, M10-N2-mapBytes: kırmızı |
> | N-3 | "13 sabit" | AST ile yeniden sayıldı: `operatorauth` 9 sabit + 8 sentinel; `internal/db` 1 sabit + 2 sentinel değişken; 9. tur bloğunda düzeltildi | — |
> | N-4 | `go list` hatası sebebini söylemiyordu | `ExitError.Stderr` mesajda; gerekçe yorumda (paket/modül/araç zinciri teşhisi; ortamı yankılamaz; bir modül indirme hatası proxy URL'sini — burada genel varsayılan — adlandırır) | — |
> | N-5 | `compareDummy` yorumu var olmayan bir "erken uzunluk çıkışı" anıyordu | x/crypto v0.54.0 okundu: `CompareHashAndPassword`'de uzunluk çıkışı YOK (yalnız `GenerateFromPassword` 72 baytı reddeder); ölçüldü: cost 12'de 8, 72, 73, 200 ve 4 096 bayt 196–212 ms. Kırpma SAVUNMA olarak tutuldu (iki kol aynı girdiyi hash'ler; ileride kısa yol açan bir karşılaştırıcıya karşı), yorum ölçülen gerçekle yazıldı | M10-N5-noClamp: **yeşil — adıyla eşdeğer**; `TestPassword_EveryArmPaysOneComparisonAtTheSameCost` etkilenmedi |
>
> **10. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı; probe
> içeriği `key.go`'nun sonuna eklenip geri yazıldı, repoya yeni dosya konmadı):** 9. denetçinin
> 41'i — **40 kırmızı + 1 eşdeğer yeşil** (A9-sun-Preview-method); bu turun 10'u — 6 kırmızı
> + 3 beklenen yeşil (iki eşdeğer kontrol, clamp) + 1 kontrol (yukarıda); 8. denetçinin 45'i
> (+ 9. turunkiler) — 43 kırmızı, B-limits-byValue-unfilled beklenen yeşil, C5b beklenen
> derleme kırmızısı; 8. turun 11'i — 10 kırmızı + N5 kontrolü; ağır örnekler 14/14 kırmızı:
> D4, D5, X8, X2, M1, H1, L1, R3, tip geri dönüşleri ve `limits limits`.
>
> Kapı zinciri baştan, staticcheck SON değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race
> -count=1 ./...`, 2026-10-01 02:52 UTC, yük 2,9–4,8): **bu koşuda** tek kırmızı T72
> (`cmd/rotatekek`); `internal/operatorauth` 181,4 sn, `internal/db` 106,4 sn, duvar 289 sn.
> Kapsam: `internal/operatorauth` %95,6 (44 test), `internal/sun` %97,4.
>
> **11. tur (2026-10-01) — 10. denetçi (bağımsız) RED.** Bloklayıcılar: **B-1** —
> `authKeys` istisnasının gerekçesi ("matris onu kapsar") yanlıştı: `Authenticator` specimen'i
> KEK, HMAC anahtarı ve adresi arıyordu; tuttuğu türetilmiş challenge anahtarını ve
> `dummyDigest`'i aramıyordu. Kural (2) bir seviyede duruyordu (`[][]byte`, `[2][]byte`,
> `*[][]byte`, `[][32]byte` — R2a–R2e) ve `allowedFields`'ın "a Key" gerekçesi denetlenmeyen
> bir iddiaydı: CK (`challengeKey Key` → `challengeKey [][]byte`, aynı ad, girdi dokunulmadan)
> 44/44 yeşildi. **B-2** — kayıt `operatorauth` ve `internal/db` ile sınırlıydı:
> `func E() sun.EV2Auth` ve `var Auth *sun.EV2Auth` (`KeyENC`/`KeyMAC` düz `[]byte`) yeşildi.
> **Orkestratörün kararı:** (1) `allowedFields` alanın TİPİNİ de pinler; (2) kural (2) eleman
> ve map değerine özyinelemeli, eleman işaretçisi adıyla muaf *(12. turda düzeltildi: muafiyet
> fmt'nin davranışıyla çelişiyordu)*; (3) specimen sırları tam —
> türetilmiş anahtar eklenir, kapsama mekanik pinlenir, `dummyDigest` adıyla sınır; (4) kayıt
> modül geneli; (5) **teslimden önce kendi kendini denetleme** — bu turda yazılan her yorum,
> gerekçe, sayılı sınır ve kart cümlesi için "onu yanlışlayan tek satırlık bir mutant var
> mı?", yeni mekanizmalara karşı en az 10 kaçış denemesi.
>
> **Yapılan (yalnız test ve belge; ürün kodu değişmedi):**
>
> - **Tip pini (karar 1).** `allowedFields` girdisi `{typ, why}`; alan testi her alanın
>   `typeID`'sini girdiyle karşılaştırır, uyuşmazlık kırmızı (*"… is now a …; allowedFields
>   pins … -- a type changed under the same name"*). `typeID` her adlı tipi TAM paket yoluyla
>   yazar (bir build'de tektir) ve adsız tipin değişince tipi değiştiren her parçasını:
>   kanal yönü, alanın paket yolu, etiketi ve gömülülüğü, fonksiyonun parametreleri ve
>   variadic'liği, arayüzün yöntemleri. Kapalı kümenin adları da `operatorauth` ve `db` dışında
>   TAM yolla (modüle göreli bir yol bir standart kütüphane yoluna — `internal/poll` — eşit
>   olabilirdi).
> - **Kural (2) özyinelemeli (karar 2).** `isBytesOrText` dilim, dizi ve map değerinde her
>   derinliğe iner; adsız struct elemanının alanlarını okur; bir kaba işaretçiyi açar; HER
>   genişlikte tamsayı dizisi düz sayılır (kendi kaçış denemem: `[]uint16` UTF-16 metin taşır ve
>   `[]byte` ile aynı ondalıkları basar). Okumadıkları adıyla md. 18 tek listesinde (S3, S4).
> - **Alan yürüyüşü her paketin yapısına girer** (`heldStructs`): işaretçi, dilim, dizi ve
>   map (anahtar ve değer) üzerinden tutulan HER struct, `okFieldTypes` dışında, hangi paketin
>   olursa olsun yürünür. Kendi kaçış denemelerim, eski kodda ölçülerek YEŞİL: `sun.EV2Auth`'u
>   `notSpecimens`'e bir gerekçeyle koymak (yürüyüş yalnız iki paketin yapısına giriyordu, düz
>   `KeyENC` okunmuyordu), `last http.Cookie` ve `last []http.Cookie` (düz `Value`).
> - **Specimen araması mekanik (karar 3).** `Authenticator` specimen'i türetilmiş challenge
>   anahtarını arar; `TestSpecimens_SearchEveryRedactedValueTheyHold`: specimen'in TUTTUĞU her
>   redakte yaprak (`*string`) — yapı, işaretçi, arayüz, dilim, dizi ve map üzerinden — ya
>   aranan sırlardan biridir ya da `unsearched`'te gerekçesiyle adlıdır; bayat `unsearched`
>   girdisi kırmızı; DEĞERSİZ bırakılmış yaprak kırmızı; yaprak tutabilen bir İSTİSNA bir
>   specimen'in içinde tutulmak zorunda. `dummyDigest` adıyla aranmaz (S6). Kendi kaçış
>   denemelerim, eski kodda ölçülerek YEŞİL: `Config`'e specimen'in doldurmadığı `Extra Key`
>   (değersiz yaprak), `authKeys`'e `extra []Key` (yürüyüş dilime inmiyordu), `Identity`'ye (bir
>   istisna) `k Key` (hiçbir specimen tutmuyordu — kontrol: denetim kapatılınca yeşil).
> - **Kayıt modül geneli (karar 4).** `github.com/atknatk/tappa/` altındaki her paketin adlı
>   tipi kaydedilir. Bugünkü ölçüm **+0 / −0**: küme aynı — specimen 13 (`SessionToken`,
>   `Challenge`, `Secret`, `EnrollmentToken`, `PendingBlob`, `Issued`, `Pending`, `Key`,
>   `Authenticator`, `Config`, `db.OperatorAccount`, `db.SealedSecret`, `db.PasswordHash`),
>   istisna 8 (`Identity`, `Store`, `db.OperatorSession`, `db.OperatorAuthEvent`, `authKeys`,
>   `limits`, `budget`, `budgetWindow`). 10. turda eşdeğer yeşil sayılan A9-sun-Preview-method
>   artık KIRMIZI (`sun.Preview`'un kendisi kaydedilir). Süre, `apiTypes` bütünü: sıcak
>   0,18–0,34 sn (`-race` 0,30–0,48 sn), soğuk 0,45 sn (`-race` 0,71 sn) — yük ~4–8 altında;
>   kural ≤ ~5 sn.
> - ~~**Gerekçeler yalnız mekanik iddialar.**~~ *(12. turda yanlışlandı: "each field a Key",
>   "each field a *budget", "a count and a start time" denetlenmeyen iddialardı — uuid ve
>   fonksiyon alanları kural (1)'in dışındaydı, K5d (`authKeys`'te KEK'in iki yarısını tutan iki
>   uuid) 45/45 yeşildi. 12. tur: kural (1) istisnasız; gerekçe metni gözden geçirenin
>   iddiasıdır — md. 18 S11.)* Dört iç istisnanın gerekçesinden "yalnız bir
>   `Authenticator`'ın parçası olarak basılır" ve "matris onu kapsar" kalktı (analizle, ölçülmedi:
>   `Config`'e bir `authKeys` alanı ekleyen tek satırlık bir mutant yanlışlardı); yerine tip pini ve arama pinine
>   atıf. `budget.windows` gerekçesi "opaque rate key" değil, ölçülen anahtarlar: istemci
>   adresi, operatör id'si, süreç geneli bütçenin boş anahtarı (`flow.go`).
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B-1 | `authKeys` gerekçesi, kural (2) bir seviye, denetlenmeyen "a Key" | Karar 1–3 (yukarıda) | CK — üç bağımsız kırmızı: tip pini *"authKeys.challengeKey is now a [][]uint8; allowedFields pins github.com/atknatk/tappa/internal/operatorauth.Key"*, kural (2) *"holds text or bytes in the open ([][]uint8)"*, matris *"Authenticator leaks on Sprintf %w · S1 the value itself"* (anahtar artık aranıyor). R2a–R2d kırmızı (*"Authenticator.r holds text or bytes in the open ([][]uint8)"* …); R2e (`[]*[]byte`) ~~**yeşil, doğru** — S3~~ *(12. turda yanlışlandı: `badVerb` eleman işaretçisini açar; 12. tur bloğu)* |
> | B-2 | Kayıt iki paketle sınırlı | Karar 4 | B2-E, B2-Auth: *"github.com/atknatk/tappa/internal/sun.EV2Auth is reachable from this package's exported API and is neither a specimen nor a named exception"* |
> | N-1 | Sınır listeleri ayrışmıştı (üçüncü sınır yalnız test yorumunda) | md. 18 **tek liste** (P1–P7, S1–S10); ADR 0020 notu, OP-8 devri ve test yorumları (`leak_external_test.go` üç yer, `units_test.go`) yalnız işaret eder; 10. tur bloğunun "kalan sınır" cümlesi işaretle | — |
> | N-2 | "1 953 tip" platforma bağlı | 10. tur bloğunda üstü çizildi; sayı yazılmıyor, test `t.Logf` ile basar | — |
> | N-3 | `go list` stderr yorumu | `cmd/go/internal/web/api.go` okundu: proxy URL'si `url.URL.Redacted` ile basılır (parola maskelenir); maskelenmeyen tek şey kullanıcı adı kısmındaki bir kimlik bilgisi — adıyla | — |
> | N-4 | Mk düz dönüş | md. 18 S5, ölçülerek: `func Snap() struct{ K []byte }` ve `func C() *http.Cookie` **yeşil** | — |
>
> **Kendi kendini denetleme (karar 5) — 30 kaçış denemesi** (hepsi kopyala-geri-yaz, sha
> doğrulandı; aynı-ad sondası yalnız scratchpad kopyasında):
>
> | Mekanizma | Deneme | Sonuç |
> |---|---|---|
> | Tip pini | SA1 takma ad (`challengeKey keyAlias`, `type keyAlias = Key`) | **yeşil, doğru** — takma ad aynı tiptir |
> | | SA2 `*Key`; SA13 girdinin tipi `*Key`, alan `Key` | kırmızı (tip pini) |
> | | aynı TypeString başka paketten: scratchpad kopyasında `internal/opx/operatorauth.Key` — `reflect.String()` ikisinde de `operatorauth.Key` | kırmızı: *"authKeys.alt is now a github.com/atknatk/tappa/internal/opx/operatorauth.Key; allowedFields pins github.com/atknatk/tappa/internal/operatorauth.Key"* |
> | | SA20 kanal yönü (`<-chan string`, pin `chan string`); SA21 alan etiketi | kırmızı — **ikisi de okurken bulundu**: ilk `typeID` yönü ve etiketi yazmıyordu; ölçümden önce düzeltildi |
> | | modüle göreli ad bir stdlib yoluna eşit (`internal/poll`) | okurken bulundu, yapıyla kapandı (tam yol); mutantı ölçülmedi |
> | Özyineleme | SA3 `[][][]byte`, SA4 `map[string][]string`, SA5 `[]struct{ s string }`, SA19 `[]uint16` | kırmızı (SA19 okurken bulundu, ölçümden önce kapandı) |
> | | R2e `[]*[]byte`, SA6 `map[string]*string` | **yeşil, adıyla** — S3 *(12. tur: R2e için yanlış — ölçülmemiş bir fmt iddiasına dayanıyordu, kendi denetimim yakalamadı; SA6 doğru, ölçüldü)* |
> | | SA7 `[]uuid.UUID`, SA24 `[]float64` | **yeşil, adıyla** — S4 |
> | Alan yürüyüşü | SA14 `sun.EV2Auth` istisna olarak; SA15 `http.Cookie`; SA16 `[]http.Cookie` | eski kodda **YEŞİL — kaçış**; şimdi kırmızı (*"github.com/atknatk/tappa/internal/sun.EV2Auth.KeyENC holds text or bytes in the open ([]uint8)"*, *"net/http.Cookie.Value …"*) |
> | Modül kaydı | SA8 `func Cfg() config.Config` | kırmızı: *"github.com/atknatk/tappa/internal/config.Config is reachable …"* |
> | | LIM-S1 `var Any any = sun.EV2Auth{}`; LIM-S2 `Key`'in dışa kapalı yöntemi `db.ResolvedAdmin` döndürür; LIM-S5 `Snap`, `C() *http.Cookie` | **yeşil, adıyla** — S1, S2, S5 |
> | Arama pini | SA9 challenge anahtarı aranmaz; SA10 `authKeys`'e beşinci `Key`; SA11 bayat `unsearched`; SA12 `dummyDigest` girdisi silinir | kırmızı |
> | | SA17 değersiz yaprak (`Config.Extra`); SA18 `[]Key`; SA25 istisnada `Key` | eski kodda **YEŞİL — kaçış**; şimdi kırmızı (SA25 kontrolü: denetim kapatılınca yeşil) |
> | | SA22 nil `*Key`; SA23 `map[string]Key` | kırmızı |
>
> Ayrıca okunarak düzeltilen kendi yazılarım: `allowedFields`'ın başlığı yanlış bildirime
> (`type allowedField`) yapışmıştı; `budget.windows` gerekçesi ("opaque"); iç istisna
> gerekçeleri (yukarıda). Kırmızı çizgi taraması kendi `typeID`'mi yakaladı (R7b: bir fmt
> çağrısında `f.Name` — alan adı, kişisel veri değil); muafiyet yerine değişken adı değişti.
>
> **11. tur mutasyon koşuları (hepsi kopyala-geri-yaz, her birinde sha doğrulandı; probe
> içeriği `key.go`'nun sonuna eklenip geri yazıldı, repoya yeni dosya konmadı):** bu turun 39'u —
> B-1/B-2'nin 8'i, kaçış denemelerinin 29'u (aynı-ad sondası scratchpad kopyasında), SA25'in
> kontrolü ve yeni girdi biçimine çevrilen `M8-N1-runesOnly` — **29 kırmızı + 10 beklenen
> yeşil** (R2e, SA1, SA6, SA7, SA24, SA25 kontrolü, dört LIM); 9. denetçinin 41'i — **41
> kırmızı** (A9-sun-Preview-method dahil, yukarıda); 10. turun 10'u — 10. turdaki gibi (6
> kırmızı + 3 beklenen yeşil + 1 kontrol); 8. denetçinin 45'i — 43 kırmızı,
> B-limits-byValue-unfilled beklenen yeşil, C5b beklenen derleme kırmızısı; 8. turun 11'i —
> 10 kırmızı + N5 kontrolü; ağır örnekler **14/14 kırmızı**: D4, D5, X8, X2, M1, H1, L1, R3,
> tip geri dönüşleri (dördü artık arama pinini de kırar: yaprak `*string` değil) ve `limits
> limits`. Eski listelerin `allowedFields` satırları yeni `{tip, gerekçe}` biçimine çevrildi
> (11 satır, `M8-N1-runesOnly` dahil; tip, mutantın eklediği alanın `typeID`'si — kural (2) tek
> başına sınansın diye).
>
> Kapı zinciri baştan, staticcheck SON değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race
> -count=1 ./...`, 2026-10-01 04:16 UTC, yük 2,6–4,3): **bu koşuda** tek kırmızı T72
> (`cmd/rotatekek`); `internal/operatorauth` 166,9 sn, `internal/db` 39,5 sn, duvar 278 sn.
> Kapsam: `internal/operatorauth` %95,6 (45 test), `internal/sun` %97,4.
>
> **12. tur (2026-10-01) — 11. denetçi (bağımsız) RED.** Doğruladıkları: ürün sondası 64 306
> render, 0 isabet (ürün kodu değişmemiş); kendi 64 mutasyonunun çoğu kırmızı (tip pini, kural
> (2), `heldStructs`, modül kaydı); 14/14 ağır ve CK (üç yoldan) kırmızı; CI'ya yakın koşu ve
> ağsız Docker temiz. **Bloklayıcılar:** **B-1** — S3'ün gerekçesi yanlıştı: `badVerb` bir
> eleman ya da map-değeri işaretçisini (dilim/dizi/map'e) her derinlikte BİR KEZ açar;
> `authKeys.extra []*[]byte` türetilmiş bir anahtarla, tipi pinli: bütün testler yeşil, 269
> render'ın 24'ünde anahtar. **B-2** — `okFieldTypes` ve fonksiyon tipli alanlar kural (1)
> ile (2)'nin dışındaydı ve bu tek listede yoktu: K5d (`authKeys`'te KEK'in iki yarısını tutan
> `hintA, hintB uuid.UUID`, test dosyasına dokunmadan) 45/45 yeşil, 36/269 render'da iki
> yarı; C5b, C1, C3 de yeşil. **Orkestratörün kararı: gerekçe sınıfını YAPIDAN kır** —
> (1) kural (1) istisnasız; (2) gerekçe metinleri gözden geçirenin iddiası olarak ilan
> edilir; (3) S3 düzeltilir, muaf biçimler ölçülerek pinlenir.
>
> **Yapılan (yalnız test ve belge; ürün kodu değişmedi):**
>
> - **Kural (1) istisnasız.** Alan yürüyüşü her alanı — fonksiyon ve `okFieldTypes` tipliler
>   dahil — adıyla ve tipiyle `allowedFields`'ta ister; 17 yeni girdi (`Authenticator.now/
>   log/compareFn/digestFn`, `budget.mu/period/now`, `budgetWindow.start`, `Config.Now/Log`,
>   `Identity`, `Issued`, `db.OperatorAccount` ve `db.OperatorSession`'ın uuid'leri,
>   `db.OperatorAccount.LockedUntil`). `okFieldTypes` yalnız kural (2)'nin okumadığı ve
>   yürüyüşün girmediği tiplerdir (`heldStructs` artık onlarda durur — önce `*slog.Logger`'ın
>   içine giriyordu, ilk koşu `log/slog.Logger.handler`'ı adsız buldu).
> - **Gerekçe metinlerinin statüsü (S11).** `notSpecimens`'in başlığı ilan eder: bu dosyadaki
>   her `why` gözden geçirenin iddiasıdır; testler ad, tip ve küme üyeliğini pinler, metni
>   asla. Dört iç istisnanın gerekçesi yalnız mekanik doğru olanı söyler ("alanları
>   `allowedFields`'ta adıyla ve tipiyle"). `unsearched` girdileri bir yol listesiyle pinli.
> - **S3 — işaretçi modeli ölçülerek.** `isBytesOrText(tip, dışa açık mı)`: fmt bir yolda bir
>   işaretçiyi açar (dizi, dilim, struct, map gösteriyorsa, her derinlikte), sonrakiler
>   adrestir; `encoding/json` dışa açık alanın her işaretçisini izler (kendi kaçış denemem:
>   dışa açık `*string` `json.Marshal`'da basılıyordu). String türünde olmayan map anahtarı da
>   okunur. Pin: `TestExportedTypes_ExemptFormsPrintNoKeyBytes` — 19 biçim KEK'le dolu, matriste:
>   muaf on biçim (`*string`, `**[]byte`, `[]*string`, `map[string]*string`,
>   `map[*string]bool`, `[]**[]byte`, `*[]*[]byte`, `[]*[]*[]byte`, `chan []byte`,
>   `func() []byte`) **0 yolda**; okunan dokuz biçim (`[]byte` 262, `*[]byte` 144,
>   `[]*[]byte` 144, `map[string]*[]byte` 144, `[]*[32]byte` 144, `map[[32]byte]bool` 262,
>   `[]*struct{ b []byte }` 144, dışa açık `*string` 3, dışa açık `**[]byte` 3 yolda); ve
>   `isBytesOrText`'in cevabı ölçümle eşleşir.
> - **N-1 — bütçe anahtarları pinli.** `checkBudgetKeys`, `TestLeak_NoInputInAnyErrorOrLogLine`'ın
>   42 kolu koştuktan sonra (+ bir `AllowRequest`) beş bütçenin anahtarlarını yansımayla okur:
>   flood/work istemci adresi (`netip.ParseAddr`), account operatör id'si (`uuid.Parse`),
>   auditCap/enroll yalnız `""`; anahtar mesajda basılmaz, yalnız uzunluğu. Bugün 17 anahtar.
>   **Sapma:** karar "iç test" diyordu; harici testte, çünkü 42 kolun hepsini DB'siz süren
>   tek harness orası — iç bir test aynı kapsam için DB ya da yeni bir sahte store isterdi;
>   yansıma dışa kapalı alanı yalnız okur.
> - **N-2 — değersiz yer.** Yaprak tutabilen bir tipe nil işaretçi, boş dilim ve boş map da
>   "değersiz" (`holdsLeaf`).
> - **N-3 — S6 pinli.** `TestDummyDigest_IsNotTheDigestOfAKnownValue` (iç): sahte digest sıfır
>   tohumun ve boş parolanın digest'i değil (iki karşılaştırma yan yana; `sharedDummy`,
>   ek üretim yok; `-race` altında ~2,6 sn). "İki build'in digest'leri farklı olmalı"
>   seçilmedi: bcrypt her digest'i tuzlar, aynı tohumun iki digest'i de farklıdır — hiçbir
>   şey kanıtlamaz. SP3 bir küme değişikliğidir (`unsearched`'e yeni girdi): yol listesi pini
>   onu kırmızı yapar.
> - **N-4 — tek liste tek kaynak.** S11 (gerekçe statüsü), S12 (`go list` stderr'inde URL'nin
>   kullanıcı adı), S13 (pozitif kontrolün yalnız-adres yolları), S14 (tip pini adla — kendi
>   denetimimden) eklendi; S3, S4, S6, S7 yeniden yazıldı. Tarama (`limit`, `COUNTED`,
>   `NOT CLAIMED`, `not seen`, `exempt`, `not walked`, `not masked`, `cannot show`, `says
>   nothing`): test yorumlarında tek listede olmayan sınır kalmadı; sınır anan her yorum
>   listeye işaret eder.
>
> | Bulgu | Neydi | Karşılığı | Kıran mutasyon |
> |---|---|---|---|
> | B-1 | S3: "fmt kaptaki işaretçiyi adres basar" | İşaretçi modeli + ölçüm testi (yukarıda) | `authKeys.extra` türetilmiş anahtarla, tipi pinli: `[]*[]byte`, `map[string]*[]byte`, `[]*[32]byte` — *"authKeys.extra holds text or bytes in the open ([]*[]uint8)"* …; 11. turun R2e'si (`[]*[]byte`, "yeşil, doğru") artık kırmızı |
> | B-2 | `okFieldTypes`/fonksiyon alanı kural (1)'in dışında | Kural (1) istisnasız | K5d *"authKeys.hintA / hintB is a field nothing authorised"*; C5b *"limits.id …"*; C1 *"Identity.ExtraID …"*; C3 *"db.OperatorSession.ExtraID …"*; C4 (fonksiyon) *"authKeys.hook …"*, C4 (zaman) *"authKeys.made …"* — hepsi test dosyasına dokunmadan |
> | N-1 | Bütçe anahtarı gerekçesi pinsiz | `checkBudgetKeys` | C7d *"the flood budget holds a key that is not a client address (25 characters; not printed …)"*; C7e *"the account budget holds a key that is not an operator id (99 characters …)"* (+ `flow_db_test.go`'nun hesap bütçesi kolu) |
> | N-2 | Nil işaretçi değersiz sayılmıyordu | `holdsLeaf` ile nil işaretçi, boş dilim, boş map | `Config.Extra` `*struct{ K Key }`, `**Key`, `*[1]Key` nil: *"Config holds no value at Config.Extra, where a redacting leaf can sit"* |
> | N-3 | S6 pinsiz; SP3 | `TestDummyDigest_IsNotTheDigestOfAKnownValue`; `unsearched` yol listesi | `rand.Read(nil)`: *"the dummy digest is the digest of the zero seed, encoded as newDummyDigest encodes it"*; SP3: *"the specimens leave ["Authenticator.keys.challengeKey" "Authenticator.keys.dummyDigest"] unsearched; the pinned list is ["Authenticator.keys.dummyDigest"]"* — kontrol: pin kapatılınca SP3 yeşil |
> | N-4 | Tek liste tek kaynak değildi | S11–S14; yorumlar işaret eder | — |
> | Ölçüm testinin kontrolleri | — | — | işaretçi açma dalı silinir: *"isBytesOrText([]*[]byte) = false, want true"* (+ dört biçim daha); JSON kuralı silinir: *"isBytesOrText(exported *string) = false, want true"*; `fieldShapes` `[]*[]byte`'ı muaf işaretler: *"isBytesOrText([]*[]byte) = true, want false"* ve *"[]*[]byte prints the key on Sprintf %s · S1 the value itself"* |
>
> **Kendi kendini denetleme — 19 deneme** (hepsi kopyala-geri-yaz, sha doğrulandı):
>
> | Mekanizma / iddia | Deneme | Sonuç |
> |---|---|---|
> | Kural (1) istisnasız | SA12-1 gömülü `uuid.UUID` (KEK'ten dolu); SA12-2 adsız struct alanı `x struct{ a uuid.UUID }`; SA12-3 generic örnek `atomic.Pointer[Key]` (alanın kendisi tipiyle adlı) | kırmızı: *"authKeys.UUID …"*; *"authKeys.x …"*, *"authKeys.x.a …"*; *"sync/atomic.Pointer[…operatorauth.Key]._ / .v is a field nothing authorised"* |
> | | SA12-4 `Identity.AdminID` → `string` | derlenmiyor (testler uuid karşılaştırır) — yerine SA12-4b `Identity.SessionID` → `[16]byte` (iki yönde atanabilir): kırmızı, tip pini + kural (2) |
> | S3 muafiyetleri | SA12-5 `**[]byte`, SA12-6 `map[string]*string`, SA12-7 `chan []byte` — üçü de KEK'le dolu (matris KEK'i arar); SA12-16 `[]*[]*[]byte` | **yeşil, adıyla** — S3 (ölçümü ayrıca `TestExportedTypes_ExemptFormsPrintNoKeyBytes`) |
> | | SA12-15 `map[[32]byte]bool`, KEK anahtar | kırmızı: matris + kural (2) |
> | JSON kuralı (okurken bulundu) | SA12-8 dışa açık `Config.Hint *string`; SA12-9 dışa açık `Issued.Hints []*string` | kırmızı: *"Config.Hint holds text or bytes in the open (*string)"* … |
> | Ölçüm pini | SA12-18 `fieldShapes` yanlış etiket | kırmızı (yukarıda) |
> | Gerekçe statüsü (S11) | SA12-10 `dummyDigest` gerekçesi yanlış yazılır ("the KEK itself, printed everywhere") | **yeşil, adıyla** — S11: tanım gereği sözleşmenin dışında |
> | Değersiz yer | SA12-11 boş `[]Key{}`; SA12-14 istisna `Identity`'de nil `*Key` | kırmızı: *"holds no value at Authenticator.keys.ks …"*; *"the exception Identity can hold a redacting leaf that no specimen holds"* |
> | Bütçe anahtarları | SA12-12 account `id + "x"`; SA12-17 flood `addr + "|" + addr` | kırmızı (*"… not an operator id (37 characters …)"*, *"… not a client address (19 characters …)"*) |
> | Sahte digest (S6) | SA12-13 sabit, sıfır olmayan tohum | **yeşil, adıyla** — S6 |
>
> Okuyarak denetlenen ve mutantı olmayanlar: tip pini adlı yapı-olmayan bir tipin TANIMINI
> görmez — S14 olarak listeye girdi (bugün yürünen alanlarda yalnız `uuid.UUID`,
> `time.Duration`); işaretçi modeli ALAN içindir — doğrudan basılan bir değerin kendisi
> derinlik 0'da fiil korunarak açılır, bu alan kuralının değil matrisin konusudur (specimen'ler
> matriste doğrudan basılır); adsız bir struct'ın dışa açık alanı, dışa kapalı bir alanın
> altındaysa JSON'a ulaşmaz ama kural JSON kuralıyla okur — fazla kırmızı, eksik değil.
>
> **12. tur mutasyon koşuları (hepsi kopyala-geri-yaz, sha doğrulandı; probe içeriği ürün
> dosyalarına eklenip geri yazıldı, repoya yeni dosya konmadı):** bu turun 37'si + SA12-4b —
> denetçinin istediği 16'sı **16/16 kırmızı**, üç kontrolün ikisi kırmızı (ölçüm testi) ve
> biri beklenen yeşil (pin kapalı SP3), kendi 18 denemem 11 kırmızı + 6 adıyla yeşil + 1
> derlenmeyen (yerine 4b kırmızı); ağır örnekler ve CK **15/15 kırmızı** (D4, D5, H1, L1, X2,
> X8, M1, R3, dört tip geri dönüşü ve `Secret` baytları, `limits limits`, CK); önceki
> turların 145'i (yeni girdi düzenine göre yeniden çapalandı) — **130 kırmızı**, 14 beklenen
> yeşil (B-limits-byValue-unfilled, LIM ×4, M10'un üç eşdeğer kontrolü, M8-N5 kontrolü, SA1
> takma ad, SA24, SA25 kontrolü, SA6, SA7), C5b beklenen derleme kırmızısı; 11. turda "yeşil,
> doğru" sayılan R2e artık kırmızı.
>
> Kapı zinciri baştan, staticcheck SON değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race
> -count=1 ./...`, 2026-10-01 05:49 UTC, yük 3,2–4,9): **bu koşuda** tek kırmızı T72
> (`cmd/rotatekek`); `internal/operatorauth` 173,3 sn, `internal/db` 40,2 sn, duvar 281 sn.
> Kapsam: `internal/operatorauth` %95,6 (47 test), `internal/sun` %97,4.
>
> **12b (ONAY sonrası kapanış, 2026-10-01) — 12. denetçi ONAY; dört bloklamayan kapandı.**
> (1) `checkBudgetKeys` anahtarları BİÇİMLE değil İÇERİKLE pinler (sürüşün kullandığı
> adresler, operatör id'leri, `""`) ve sürüşe `Verify`/`Logout` başarı yolları eklendi —
> D3–D7 kırmızı (*"the flood budget holds a key that is not a client address the drive used
> (43 characters …)"*, *"the account budget holds a key that is not an operator id of the
> drive (32 characters …)"* …); sınır S4'te adıyla: yalnız sürülen yollar. (2) `okFieldTypes`
> literal listeyle pinli — `okFieldTypes += [][]byte` + `authKeys.extra [][]byte`: *"okFieldTypes
> is [… "[][]uint8" …]; the pinned list is […]"*; S11'in kümelerine eklendi. (3) Dışa açık
> paket değişkeni yalnız `error` olabilir (bugün 8 sentinel) — `var LastKEK []byte`: *"an
> exported package variable LastKEK []byte: only sentinel errors are exported variables
> here"*; S5'e yazıldı. (4) ADR 0020'deki bayat aralık kalktı (aralık yazılmıyor, md. 18'e
> işaret). Ağır örnekler D4 (ham token), X8, M1, CK, K5d kırmızı. Kapı zinciri baştan,
> staticcheck son değişiklikten sonra (rc=0); tam koşu (`.env`, `-race -count=1 ./...`,
> 2026-10-01 06:41 UTC): **bu koşuda** tek kırmızı T72; `internal/operatorauth` 177,2 sn.
>
> **12c (güvenlik denetimi kapanışı, 2026-10-01) — `tappa-security-auditor` ONAY (kritik/yüksek
> yok, §4/§7 temiz); bir ORTA, iki DÜŞÜK kapandı; ÜRÜN KODU değişti.** (ORTA) `enrollAddr`
> 3/10 dk adres başına, `enroll`'dan önce (`limits.go`, `flow.go`; md. 8 ve OP-8 devri) — sızıntı
> testinde yeni kol **E10** (43 kol), `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter`'da
> kontrollü yeni kol (tek adres 4. istekte ret: digest 0, DB çağrısı 0, satır 0, süreç sayacı
> kımıldamaz; başka adres kalanı harcar), `TestBudgets_TheShippedNumbersArePinned`'te 3/10 dk, `checkBudgetKeys`'te anahtarı
> istemci adresi; kalan sınır P8. Adres payı, aynı adresten üçten fazla enrollment tamamlayan
> `TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens`'ı kırdı: ret vakaları kendi adresine
> (`192.0.2.11`) alındı — testin konusu bütçe değil. (DÜŞÜK) Parola adımı başarıda tek Info satırı:
> `operator first factor verified; second factor pending` + `operator_id` — adres, e-posta, sır yok
> (metin "password" sözcüğünü taşımaz: kırmızı çizgi taramasının R7 tetikleyicisidir — ilk hâli
> R7'de kırmızıydı, muafiyet yerine metin değişti);
> `checkPasswordVerifiedLines` her satırın bu satır olduğunu, yalnız `operator_id` taşıdığını ve
> her doğru parola için her handler'da bir kez bulunduğunu pinler (A1'in "tek satır" varsayımı
> buna göre düzeltildi); kalıcı `password_ok` türü P9. (DÜŞÜK) Bekleyen blob artık
> `HMAC-SHA256(KEK, "taptime/operator/enrollment-pending/v1/key-derivation")` ile mühürlenir
> (`authKeys.pendingKey`, bir `Key`); saklanan zarf ham KEK'le kalır; etiket
> `TestPendingBlob_IsNotTheStoredEnvelope`'ta literal ve sızıntı testinde yeniden yazılı (G11'e
> ve `Authenticator` specimen'inin sırlarına girer). **Mutantlar:** adres payı silinir — kırmızı
> (*"request 4 of one address: … want ErrThrottled from its enrollment share"*, *"E7: no
> request from another address passed …"*); sıra ters — kırmızı (*"process counter 6 -> 7;
> want … unmoved"*); sayı 10 — kırmızı (`TestBudgets_TheShippedNumbersArePinned`, DB kolu, E7); satır silinir — kırmızı
> (*"0 text and 0 JSON line(s), want 2 of each"*); satıra e-posta — kırmızı (*"5 attributes"*);
> blob ham KEK'le — kırmızı (*"the page's blob opened under the KEK ITSELF"*); etiket boş —
> kırmızı (`TestPendingBlob_*` + *"holds a redacted value at Authenticator.keys.pendingKey that
> its secrets do not include"*); saklanan zarf türetilmiş anahtarla — kırmızı
> (`TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens`, `TestEnrollment_ANextStepFirstCodeCannotSignInAgain`).
> Ağır örnekler D4, X8, M1, CK, K5d, Y20 kırmızı. Kapı zinciri baştan, staticcheck son
> değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race -count=1 ./...`, 2026-10-01 07:27 UTC,
> log metni değiştikten sonra): **bu koşuda** tek kırmızı T72; `internal/operatorauth` 175,9 sn.
> (Önceki bir tam koşu bu satırın ilk hâlindeki kısaltılmış bir test adı atfıyla
> `TestEveryNamedTestExists`'te kırmızıydı; atıf tam adla düzeltildi, koşu tekrarlandı.) Kapsam: `internal/operatorauth`
> %95,5 (47 test), `internal/sun` %97,4.
>
> **12d (kapanış denetimi, 2026-10-01) — kapanış denetçisi ONAY (32 mutantının 29'u, ağırların
> 12/12'si kırmızı; bağımsız sonda 3 961 render, 0 isabet); dört ucuz bulgu kapandı.** (1)
> `TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter` audit tavanını enrollment kollarından
> ÖNCE tüketiyordu, bu yüzden iki "satır 0" kontrolü hiç kırmızı olamazdı; enrollment kollarından
> önce `a.limits.auditCap` tazelenir — C13 (pay reddi `enrollmentRefused` yazar): *"+1 row(s) …
> want 0, 0, 0 and unmoved"*; C13b (süreç reddi aynı): *"rows +1, want pending and 0"*; ikisi de
> kırmızı; kontrol: tazeleme olmadan ikisi de yeşil (md. 8'in "satır 0" iddiası artık ölçülüyor).
> (2) Pencere metni ölçülen hâliyle (`limits.go`, md. 8, P8, ADR 0020): anahtarın kendi penceresi
> başına 3; pencere sınırında bir süreç penceresine 5'e kadar; tek bir pencere iki anahtar ve
> önceki bir istekle tükenir; sürekli tüketim ≥4 anahtar. (3) Dışa açık `error` değişkeni:
> `errors.New(<dize literali>)` ile bildirim + yeniden yazma ya da adres alma yok (AST, `apiTypes`;
> bugünkü 8 sentinel uyar) — C8c (`var ErrLastKey error`, `New`'da KEK metniyle): kırmızı
> (*"not declared as errors.New of a string literal"*, *"an exported variable written after its
> declaration"*); doğru bildirilip `New`'da üzerine yazılan bir sentinel ve `fmt.Errorf` ile
> bildirilen biri de kırmızı; S5'e yazıldı. (4) Bayat metinler: `Config.Log`'un "tek satır"ı
> (artık iki), S6 (beş `Key`'den dördü), md. 8 (beş bütçe), ADR 0020'nin bütçe listesi
> (`enrollAddr 3`), `limits.go` başlığı (enrollment bütçeleri tasarım gereği Go'nun
> kontrollerinden SONRA, digest'ten ÖNCE — ölçülen sırayla). Ağır örnekler D4, X8, CK kırmızı.
> Kapı zinciri baştan, staticcheck son değişiklikten sonra (rc=0). Tam koşu (`.env`, `-race
> -count=1 ./...`, 2026-10-01 08:15 UTC): **bu koşuda** tek kırmızı T72; `internal/operatorauth`
> 171,1 sn, `internal/db` 100,2 sn. Kapsam: `internal/operatorauth` %95,5 (47 test),
> `internal/sun` %97,4.

> **Kart düzeltmesi (2026-10-01, OP-7 uygulaması sırasında).** Yazıldı:
> `internal/db/operatorpool.go` (`OperatorDB`, `NewOperatorDB`, `ErrOperatorUnreachable`),
> `internal/db/logparams.go` (iki havuzun başlangıç parametresi pini ve bağlantı başına geri
> okuması), `internal/db/pool.go` (`New` artık pinli — backlog T79),
> `internal/config/config.go` (dört değişken tek küme, `operatorKeySeparation`, host),
> `internal/handler/operator/` (yüzey: kapalı / ulaşılamaz / yapılandırılmış),
> `cmd/tappa/operator.go` (wiring) + `main.go`, `deploy/k8s/20-app.yaml` (dört optional
> `secretKeyRef`), `deploy/README.md` → *"Operator surface (M10 OP-7)"* runbook'u,
> `scripts/db-init/02-dev-only-password.sh` (`tappa_operator`'ın geliştirme parolası),
> `.env.example`, iki örnek Secret dosyası; ADR 0020/0021'e tarihli OP-7 notları. Migration
> YOK. Ölçüm: dev Postgres 17.10; rol sondaları `SET SESSION AUTHORIZATION` ile, katalog
> sondaları `BEGIN … ROLLBACK` içinde; bir ölçüm rol varsayılanı yazmayı gerektirdi (md. 3)
> ve sonunda `pg_db_role_setting` 0 satır. Sapmalar ve kararlar:
>
> 1. **`OperatorDB` kendisi bir `OperatorConn` DEĞİL — OP-4 bloğu *"OperatorConn'u karşılar"*
>    diyordu, ölçülerek düzeltildi.** Havuzu (`*pgxpool.Pool`) `OperatorConn`'dur ve yedi
>    yöntemin her biri `operator.go`'nun aynı adlı fonksiyonuna, argümanları sırasıyla vererek
>    devreder (SQL tek yerde). Dışa açık `Exec`/`QueryRow` olsaydı `OperatorDB`'yi tutan her
>    paket operatörün bağlantısına kendi SQL metnini gönderebilirdi ve
>    `TestOperatorSQL_OnlyBoundParameters` (yalnız `operator.go`'yu okur) onu görmezdi; şimdi
>    böyle bir çağrı **derlenmez** (*"has no field or method Exec"*). Pinler:
>    `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` (yansıma: `WithTenant` yok, `OperatorConn`
>    değil, geri çağırma parametresi ya da pgx tipi taşıyan yöntem yok) ·
>    `TestOperatorDB_IsTheStoreAndNothingMore` (yöntem kümesi = `operatorauth.Store` + `Close`,
>    Store'dan TÜRETİLİR) · `TestOperatorDB_EveryMethodDelegatesVerbatim` (AST: tek `return
>    F(ctx, o.pool, <parametreler sırayla>)`) · `TestOperatorDB_SetsNoTenantContext`.
> 2. **Rol kapısı: `session_user` VE `current_user` = `tappa_operator`, ayrıcalık yok, HERHANGİ
>    bir role üyelik yok — ve HER ortamda ret** (kart *"prod'da"* diyordu; üst küme). Ölçüldü:
>    sahibin DSN'ine `role=tappa_operator` başlangıç parametresi (pgx bilinmeyen bir sorgu
>    parametresini başlangıç parametresi yapar) `session_user = tappa_owner`, `current_user =
>    tappa_operator` verdi; `roleFactsQuery` `current_user`'ı okuduğu için onu geçirirdi.
>    Üyelik: §1 *"hiçbir rolün üyesi değil"* der; `InheritsPrivilege` yalnız ayrıcalıklı
>    ebeveyni görür (`GRANT tappa_app TO tappa_operator` → `InheritsPrivilege = false`,
>    `member_of_any_role = true`, ölçüldü — `TestOperatorRoleQuery_SeesAMembershipOfAnyKind`).
>    Her ortamda ret, çünkü geliştirmede uyarının gerekçesi (sahip olarak migration/seed/psql)
>    operatör yüzeyi için yoktur: geliştirme rolü `02-dev-only-password.sh`'ten parola alır.
>    Pinler: `TestOperatorRoleRefusal_IsTappaOperatorAndNothingMore` (doğruluk tablosu),
>    `TestOperatorDB_RefusesEveryRoleButTappaOperator` (gerçek sunucu: sahip, `tappa_app`,
>    sahibin `role=` taşıyan DSN'i; dev ve prod). *(2. tur, B9: kapı ADR 0021 §1'in rolünün
>    kalanını da okur — `rolcreatedb`, `rolcreaterole`, `rolreplication` ve rolün bir ÜYESİ
>    olması (ters üyelik); dördü de ret. Aşağıdaki "2. tur" bloğu.)*
>    ⚠️ ~~**Kapsam DIŞI, orkestratöre (backlog adayı) — ÖLÇÜLDÜ:**~~ → *(2. tur: F1 ile
>    KAPATILDI — aşağıdaki "2. tur" bloğu. Paragraf ilk turun ölçümü olarak duruyor.)* aynı açık **müşteri havuzunun**
>    kapısında duruyor. `DATABASE_URL` = sahibin DSN'i + `role=tappa_app` →
>    `roleFactsQuery` *"tappa_app, f, f, f, f"* okur, `Privileged() = false`, üretim açılışı
>    **geçer**; o bağlantıda `SET ROLE NONE` → `current_user = tappa_owner`, `rolsuper = t`
>    (`RESET ROLE` değil: başlangıç parametresi oturumun varsayılanıdır). RLS `current_user`'a
>    göre işlediği için ürün sorguları yine RLS altında; ama oturum bir SQL ifadesi uzakta
>    superuser. ~~Bu görev `pool.go`'nun rol kapısına dokunmadı (T79 yalnız pini getirdi).~~
> 3. **(a) başlangıç parametresi — ölçüldü, iki havuza birden (T79).**
>    `log_parameter_max_length_on_error = 0` her bağlantının **başlangıç parametresidir** ve
>    `AfterConnect`'te **her yeni bağlantıda** geri okunur (`current_setting` 0 için tam `"0"`
>    döner, 64 için `"64B"` — ölçüldü); ilk bağlantı açılışın ping'idir, yani reddi açılış
>    reddidir. Ölçüm (dev; `ALTER ROLE tappa_app IN DATABASE postgres SET … = -1`, sonra
>    RESET, `pg_db_role_setting` 0 satır): parametresiz bağlantı **-1**, pinli **0**;
>    `options=-c …=-1` + pin → 0; DSN'de `?…=-1` + pin → 0 (pin üzerine yazar); başka
>    veritabanına aynı rol → 0. **`log_parameter_max_length` iğnelenmedi:** `superuser`
>    bağlamlıdır; süper kullanıcı olmayan bir başlangıç paketi onu adlandırınca bağlantı
>    **42501 ile reddedilir** (ölçüldü) — iğnelemek iki havuzu da düşürürdü — ve aynı sebeple
>    DSN sahibi onu değiştiremez (`SET` ve `ALTER ROLE … SET` ikisi de 42501, `tappa_operator`
>    olarak ölçüldü). Üretim onu -1'de koşturduğu için geri okuma onu şart koşmaz. Üretimin
>    `on_error` değeri zaten 0'dı (orkestratör, 2026-09-26): pin hiçbir üretim bağlantısının
>    değerini değiştirmez. Pinler: `TestPin_TheStartupParameterOverridesARoleDefault` (rol
>    varsayılanını **commit eder**, yalnız bakım veritabanı için; Cleanup RESET eder ve 0 satırı
>    doğrular), `TestPin_ADSNCannotUnpinIt`, `TestPin_AConnectionTheParameterDidNotReachIsRefused`
>    (açılışta iki havuz + **sonraki** bir bağlantı), `TestPin_NoRoleLevelSettingOnTheConnectingRoles`
>    (T79 katalog pini: `tappa_app` ve iki operatör rolü, her veritabanında; OP-5 pininin emsali).
>    *(2. tur, B4/B5b: "DSN'de `?…=-1` + pin → 0" yalnız AYNI yazım için doğruydu; harf
>    büyüklüğü farklı bir anahtar ikinci bir harita anahtarı olarak kalıyor ve paket sırasına
>    göre kazanıyordu. Pin artık adı harf duyarsız eşleyen her anahtarı siler. Geri okuma
>    yalnız tam `"0"` metnini kabul eder; `"64B"`, `"1"`, `""` reddedilir. "2. tur" bloğu.)*
> 4. **Ulaşılamazlık açılışı DURDURMAZ — karar.** `NewOperatorDB` bağlanamadığında
>    (~~`connect` adımı: SQLSTATE, errno, DNS, bağlam~~ → *2. tur, B1: KAPALI liste — ağ, ad
>    çözümü, zaman aşımı, iptal edilmiş açılış ve SQLSTATE sınıfı 08 ve 28, `3D000`, `53300`,
>    `57P01`–`57P03`; ping'deki öteki her SQLSTATE ve ping'den sonraki her adım RET'tir ve
>    açılışı durdurur — "2. tur" bloğu; *2b:* TLS hatası ve tanınmayan hata da ret, birden çok
>    denemeli bir bağlantı ancak HER denemesi ulaşılamazsa ulaşılamaz — "2b" satırı*)
>    `db.ErrOperatorUnreachable` sarar;
>    `cmd/tappa` onu **ulaşılamaz yüzey** (503, *"unavailable"*) + ERROR satırıyla karşılar ve
>    **müşteri ürünü açılır**. Ulaşılan şeyin reddi (rol kapısı, pin geri okuması), bozuk DSN ve
>    yanlış boyda anahtar **açılışı durdurur**. Gerekçe: risk 7 / §4'ün *"müşteri paneli
>    etkilenmez"* ruhu ve bir geri yükleme yolu — rol parolaları bir veritabanı dökümünde yoktur,
>    B YOLU taze kümede `01-roles.sql` `tappa_operator`'ı yeniden NOLOGIN yaratır, emanetten
>    gelen dolu DSN **28P01** alır; ölümcül bir kuralla bu bir **müşteri kesintisi** olurdu
>    (28P01'in ulaşılamaz sayıldığı yanlış bir parolayla ölçüldü; geri yüklemenin kendisi
>    koşulmadı — sayılı sınır S-h). Pinler:
>    `TestOperatorDB_UnreachabilityIsMarkedAndNothingElseIs` (kapalı port, 28P01, 3D000, iptal
>    edilmiş bağlam) ve reddin İŞARETSİZ olduğunu söyleyen satırlar üç testte;
>    `TestOpenOperatorSurface_UnreachableKeepsTheProductUpARefusalStopsTheBoot`.
> 5. **Config — dört değişken, tek küme** (görev tablosunun OP-7 satırı üç ad sayar; OP-4
>    bloğu `TAPPA_OPERATOR_TOKEN_HMAC_KEY` ile dört — dört uygulandı). Hiçbiri → kapalı; dördü → açık; on dört kısmi
>    altkümenin her biri **reddedilir** ve eksikleri adıyla söyler
>    (`TestLoad_OperatorSurfaceIsAllOrNothing`). Anahtarlar `key32` (32 bayt base64). Ayrılık
>    (`operatorKeySeparation`, ham baytlarda, sabit zamanlı, tek çağrı yeri — envanter 2 → 3):
>    yalnız bir operatör anahtarı İÇEREN çiftler (öteki ikisi dahil); var olan çiftler
>    (ör. etiket KEK'i = davet anahtarı) bu kuralın değildir — onları reddetmek üretimde
>    ölçülmemiş yeni bir ret olurdu (`…/boundary` alt testi). Karşılaştırılan anahtar kümesi
>    `Config`'in `[]byte` alanlarından TÜRETİLİR (`TestNamedKeys_ListEveryKeyFieldOfTheConfig`).
>    **Host (yeni kural, gerekçeli):** tek yazım — küçük harfli DNS adı; şema, port, yol, kullanıcı
>    kısmı, sondaki nokta yok; normalleştirilmez, reddedilir (M5-03 dersi: kontrol ve tüketici
>    aynı yazımı görmeli) — ve **`TAPPA_BASE_URL`'in host'u olamaz** (§4'ün ayrı host'u; aynısı
>    o özelliği sessizce boşa çıkarırdı). *(2. tur, B8: kural YALNIZ bu host'u bilir; ingress'in
>    öteki müşteri host'ları — `www.taptime.mt`, `tappa.everva.com.tr` — kabul edilir. Hata
>    metni buna daraltıldı: "the customer product's canonical host"; operatör yüzeyini her
>    müşteri host'unun dışında tutmak OP-8'in — md. 9 (viii).)* Port'un istekte nasıl ele alınacağı OP-8'in kararı.
>    Hiçbir hata bir DEĞER basmaz (`TestLoad_OperatorRefusalsRepeatNoValue`: DSN, parolası,
>    anahtarlar base64/hex/ham/ondalık liste; host'a yapıştırılmış DSN ve anahtar dahil).
> 6. **`/operator` yüzeyi — ayrı paket `internal/handler/operator`** (ADR 0021 §3.6: müşteri
>    paneli operatör paketini import etmez; `TestCustomerPanel_ImportsNoOperatorPackage` —
>    doğrudan import ve `internal/db`'nin operatör adları, adları türetilerek). Kapalı ve
>    ulaşılamaz: `/operator` ve altı, her yöntem, **503** (`no-store`, `nosniff`, hâli
>    adlandıran gövde); müşteri rotaları değişmez. **Yapılandırılmış hâl OP-7'de HİÇBİR ROTA
>    SUNMAZ** (router'ın 404'ü): 503 *"not configured"* yalan olurdu, bir yer tutucu sayfa
>    OP-8'in silmeyi hatırlaması gereken bir rota olurdu. `*Authenticator` yüzeyin dışa kapalı
>    alanında, süreç boyunca tutulur; `operatorauth.Config` hiçbir yerde değer olarak tutulmaz.
> 7. **Wiring:** `openOperatorSurface` (havuz bu fonksiyondan çıkmaz; `Close` bağlı yöntem
>    değeri olarak döner) → `configuredSurface` → `operatorAuthenticator` (`NewKey` dönüşümü).
>    Pinler: `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` (AST: `NewOperatorDB` tek
>    çağrı; havuz yalnız `configuredSurface`'in ve `Close`'un; store yalnız `operatorauth.New`'un;
>    `run()` yüzeyi `httpx.NewRouter`'a verir) · `TestOperatorAuthenticator_EachKeyIsInItsOwnSlot`
>    (bir enrollment: mühürlü sır TOTP KEK'iyle açılır, token anahtarıyla açılmaz; oturum
>    hash'i token anahtarıyla HMAC) · `TestOpenOperatorSurface_PrintsNoValue` (üretimin text ve
>    JSON handler'ları, Debug: kapalı/yapılandırılmış/ulaşılamaz satırları ve her ret).
> 8. **Deploy.** Dört değişken `tappa-secrets`'tan `optional: true` (host dahil — ConfigMap her
>    deploy'da yeniden uygulanır, anahtarlar yokken orada duran bir host yarım küme olurdu);
>    `TestPackaging_TheOperatorSurfaceIsOneOptionalSecretSet` `config.OperatorSurfaceVariables()`'ı
>    okur. `kubectl` koşulmadı (ajan kuralı): *"Secret'ta yokken pod kalkar"* iddiasını manifest
>    testi ve `TAPPA_TAG_KEK_PREVIOUS` emsali taşır. **`tappa_operator`'ın parolası ayrı bir
>    Secret anahtarı DEĞİL** (ADR 0020/0021'in listesi öyle diyordu — tarihli notla düzeltildi):
>    yalnız DSN'in içinde durur ve role psql `\password` ile **stdin'den** verilir (ölçüldü,
>    `docker exec -i`, `BEGIN … ROLLBACK`: TTY yokken istem stdin'den okunur, SCRAM doğrulayıcısı
>    yazılır, geri almadan sonra rol yine NOLOGIN; `psql -1` stdin betiğini tek transaction'a
>    sarar — iki ifade aynı `pg_current_xact_id()`). OP-5 md. 15'in devri (giriş) böylece
>    kapandı; geliştirme parolası `02-dev-only-password.sh`'te.
> 9. **OP-8'e, adıyla:** (i) rotaları `Surface.Mount`'un yapılandırılmış dalına bağla; (ii)
>    host kapısı `Surface.host` ile (config port'u reddeder; isteğin port'u — dev'de
>    `localhost:8080` — OP-8'in kararı); (iii) ~~**erişim log'u:** yüzey kapalıyken
>    `/operator/*`'a gelen kimliksiz her istek 503 = `level=ERROR` `http.request`'tir ve
>    `deploy/README.md`'nin 5. kuralını (> 5 / 5 dk, `route`'a göre gruplu) bir tarayıcı
>    tetikleyebilir — sayılı sınır~~ → *2c'de KAPANDI: kapalı/ulaşılamaz yüzeyin 503'ü
>    `httpx.AnswerAsDesigned` ile tasarım olarak bildirilir ve kayıt yazılmaz; OP-8'in
>    gerçek rotaları bildirmedikçe her 5xx'leri kaydedilir — "2c" satırı*; (iv) ulaşılamaz yüzey yeniden denemez (yeniden başlatma);
>    (v) `TestCustomerPanel_ImportsNoOperatorPackage` doğrudan import'u görür —
>    `requireOperator` `httpx`'e konursa (ADR 0020 §4) müşteri paneli operatör paketine
>    `httpx` üzerinden geçişli ulaşır; (vi) `TestOperatorDB_RunsAsTappaOperator` yalnız
>    yazmayan iki ret yolunu sürer — başarılı bir `op_*` çağrısının havuz üzerinden sürülmesi
>    OP-8'in uçtan uca testlerinin işi; (vii) **çerez bildirimi:** operatörün iki çerezi
>    (`__Host-taptime_op`, `__Host-taptime_op_login`, OP-6) `/legal/cookies`'in tablosunda yok
>    ve `TestCookieNotice_ListsExactlyTheCookiesTheProductSets`'in tarayıcısı onları görmez
>    (yalnız `"tappa_…"` biçimli adları okur) — yüzey açıldığında bildirilip bildirilmeyeceği
>    ve tarayıcının genişletilmesi OP-8'in kararı; *(viii) (2. tur, B8)* **iki yönlü host kapısı
>    ingress'in BÜTÜN müşteri host'larını dışlamalı** (`deploy/k8s/40-ingress.yaml`: bugün
>    `www.taptime.mt`, `tappa.everva.com.tr`; config yalnız `TAPPA_BASE_URL`'in host'unu
>    reddeder) — operatör rotaları bir müşteri host'unda 404, müşteri rotaları operatör
>    host'unda 404; *(ix) (2d)* **`operator_surface=unavailable` ERROR açılış satırını okuyan
>    bir uyarı kuralı yok** — öneri 6. kuralın emsalinde, `body.operator_surface =
>    "unavailable"` ≥ 1 olay (`deploy/README.md` sınır 32); yüzey canlıda açılırken
>    eklensin ya da bilinçli olarak reddedilsin; *(x) (2h)* **güvenlik iddialarının biçimi:**
>    OP-8 ve sonrası güvenlik iddialarını yalnız (I) sevk edilen kodun ölçülen davranışı
>    (ölçen testin adıyla), (II) adıyla sayılan pinler/teller/kapalı kurallar ve tam olarak
>    neyi yakaladıkları, (III) açık bir "tamlık iddiası yok" cümlesi olarak yazar; gelecekteki
>    keyfi kod değişikliklerine karşı garanti gibi okunan koşulsuz cümle yazılmaz (OP-7 kart
>    düzeltmesi, "2h" satırı).
> 10. **Sayılı sınırlar (OP-7):** (S-a) üretim `tappa_operator`'a giriş verilmeden yüzey kapalı
>     kalır — bu görev canlıda hiçbir şey açmadı; (S-b) pin, başlangıç parametrelerini düşüren
>     bir ara katmanda (connection pooler) etkisizdir — geri okuma o bağlantıyı **reddeder**,
>     yani sonuç sızıntı değil hizmet reddidir *(2c, ölçülene eşitlendi: bu yalnız SUNUCU
>     ayarı için doğrudur. Geri okuma, argümanları istemci tarafında SQL metnine gömen bir
>     pgx kipini göremez — `default_query_exec_mode=simple_protocol` ile iki havuz açıldı,
>     geri okuma "0" dedi ve değerler metindeydi: sızıntı. Artık kurucular o kipi reddeder;
>     "hizmet reddi, sızıntı değil" bağlı parametre gönderen kiplerde, yani havuzların kabul
>     ettiği tek kiplerde doğrudur)* *(2f: bu, havuzun VARSAYILAN kipi için; çağrı başına
>     seçilen bir kip bu cümlenin dışındadır ve onun için tamlık iddiası yoktur — "2f"
>     satırı)* *(2g: havuzun varsayılan kipi için de koşullu: bugünkü kurucular düz çizgi ve
>     token token pinli, `pinLogParameters`'ın `AfterConnect`'i havuzun kancası kaldıkça —
>     "2g" satırı)* *(2h: bu cümle bugün sevk edilen kodun ölçülen davranışıdır; pinlerin
>     listelemediği kod değişiklikleri için tamlık iddiası yok — "2h" satırı)*; (S-c) rol kapısı bir AÇILIŞ ölçümüdür
>     (`pool.go`'nun sınırı aynen): açılıştan sonraki bir `GRANT` yeniden başlatmaya kadar
>     görülmez; (S-d) geri okuma bağlantının BAŞINDA koşar; aynı oturumda sonradan çalışan bir
>     `SET` (yalnız bu sürecin kendi kodu — operatör kodu bunu yapmaz) görülmez; (S-e) dev
>     veritabanında `tappa_operator` hâlâ NOLOGIN (ajan kuralı: rol sondası `SET SESSION
>     AUTHORIZATION` ile) — yapılandırılmış yolun gerçek girişle uçtan uca açılışı bu makinede
>     koşulmadı; taze PGDATA (CI) `02-dev-only-password.sh` ile giriş alır; (S-f) ~~müşteri
>     havuzunun `role=` açığı (md. 2) kapatılmadı~~ → *2. tur F1 ile kapatıldı (üretimde ret)*; (S-g) `config.Config` operatör anahtarlarını
>     ham `[]byte` tutar ve `%+v` ile basılır — T80, 8. tur kararı (1); OP-7 bir `Config`'i
>     biçimlendiren hiçbir yer eklemedi; (S-h) md. 4'ün geri yükleme senaryosu koşulmadı —
>     dayandığı iki olgu (dökümde rol parolası yok; `01-roles.sql` rolü NOLOGIN yaratır) pg_dump'ın
>     belgelenmiş davranışı ve repodaki SQL'dir, sonucu (28P01 → ulaşılamaz) testle ölçüldü.
> 11. **Kalıcı test verisi:** yok. `TestPin_TheStartupParameterOverridesARoleDefault` bir rol
>     varsayılanı commit eder ve Cleanup'ta geri alır (`pg_db_role_setting` 0 satır, her koşuda
>     doğrulanır; yarıda ölen bir koşu `TestPin_NoRoleLevelSettingOnTheConnectingRoles`'u
>     kırmızıya çevirir ve satırı adlandırır). Operatör tablolarına dokunan yeni test
>     (`TestOperatorDB_RunsAsTappaOperator`) danışma kilidini paylaşımlı alır (OP-6 md. 16) ve
>     yazmayan iki ret yolunu sürer.
> 12. **Mutasyon koşusu (kopyala-geri-yaz, her mutasyonun diskte olduğu `diff` ile, geri
>     yüklemesi `cmp`/`shasum` ile doğrulandı; hüküm testin kendi çıkış kodundan ve `--- FAIL`
>     satırlarından):** ilk koşu 48 mutasyon: 46 kırmızı, 1 derlenmedi (kullanılmayan bir
>     import; derlenen hâliyle yeniden koşuldu), **1 YEŞİL**: bağlantı hatasına pgx'in metnini
>     eklemek (`TestOperatorDB_RefusalsCarryNoConnectionString` yalnız parolayı arıyordu; pgx'in
>     bağlantı hatası parolayı değil kullanıcı ve veritabanı adını alıntılar) — test kullanıcı
>     ve veritabanı nöbetçileriyle güçlendirildi ve pgx'in metnini görebildiğini bir kontrol
>     kanıtlar. İkinci koşu 10 mutasyon (o ikisinin yeni hâlleri + 8 yeni): 10/10 kırmızı.
>     Üçüncü koşu 3 mutasyon (md. 13'ün muafiyeti): 3/3 kırmızı. Toplam 59 ayrı mutasyon, son
>     hâlleriyle 59'u kırmızı. Ayrıntı görevin raporunda. *(2. tur: 59'un 2. turdaki durumu ve
>     26 yeni deneme — aşağıdaki "2. tur" bloğu, md. 2T-6.)*
> 13. **Kapsam genişlemesi, gerekçeli: `internal/handler/marketing_test.go`.** İlk tam koşuda
>     `TestCookieNotice_ListsExactlyTheCookiesTheProductSets` kırmızıydı: tarayıcısı test dışı Go
>     kaynağındaki her `"tappa_…"` literalini çerez adı sayar ve yorumu *"başka hiçbir şey bu
>     biçimde değil"* der; `operatorRole = "tappa_operator"` o öncülü bozdu. Tarayıcıyı bir dizge
>     hilesiyle atlatmak yerine (agent-brief: kokudur) dar ve görünür bir muafiyet eklendi
>     (`cookieScanNonCookies`): literal + TEK dosya + gerekçe; uygulandığı her koşuda `t.Logf`
>     basar; dosyası literali artık taşımıyorsa kırmızı; aynı literal başka bir dosyada hâlâ çerez
>     sayılır (üç mutasyon, üçü kırmızı).
>
> **Kart düzeltmesi — 2. tur (2026-10-01, 1. üçüncü göz denetçisinin RED'inden sonra).**
> Değişen: `internal/db/operatorpool.go`, `logparams.go`, `pool.go`; `internal/config/config.go`
> (host hata metni + yorum); `cmd/tappa/operator.go` ve `main.go` (yorumlar; geliştirme
> uyarısına `session_user`); yeni `internal/db/operatorrefusal_test.go` ve var olan beş test
> dosyasına satırlar; `deploy/README.md` runbook'u; ADR 0020/0021'in OP-7 notları ("2. tur"
> işaretli). Migration YOK, bağımlılık YOK. Yukarıdaki md. 2, 3, 4, 5, 9, 10, 12 yerinde
> işaretlendi (üstü çizili + *2. tur* notu). Maddeler:
>
> - **2T-1 · B1 (bloke edici) — "ulaşılamaz" artık KAPALI bir liste.** İlk hâl ping'deki
>   HER SQLSTATE'i ulaşılamaz sayıyordu; denetçi `42501`, `22023`, `42704` ile sürecin AÇILDIĞINI
>   ölçtü — DSN'in istediğini REDDEDEN bir sunucu, yüzeyi sessizce "unavailable" yapıyordu.
>   Şimdi ulaşılamaz yalnız: ağ (errno, `net.Error`, sunucunun bağlantıyı kapatması), ad
>   çözümü, zaman aşımı, iptal edilmiş açılış ve SQLSTATE sınıfı **08**, sınıfı **28**,
>   **`3D000`**, **`53300`**, **`57P01`–`57P03`** (`unreachableSQLSTATEs`, her girişin
>   gerekçesiyle). Öteki her SQLSTATE ve tanınmayan her hata tipi RET'tir ve açılışı durdurur.
>   *(2b: bu cümle yalnız TEK denemeli bir bağlantı için doğruydu — pgx'in birleştirdiği
>   denemelerde errno dalı ilk eşleşmeyi alıyordu ve 2. denetçi dört karışık vakayı ulaşılamaz
>   ölçtü; ayrıca bir TLS uyarısı `net.Error` olduğu için ağ sayılıyordu. Düzeltme "2b" satırında.)*
>   Ping'den sonraki hiçbir adım (havuzun kurulumu, `Acquire`, rolün okunması) ulaşılamaz
>   OLAMAZ: `operatorStepErr` sınıflandırıcının hükmünü atar — sunucuya ulaşılmıştır, hata
>   kapalı yönde biter. Pinler: `TestOperatorConnectErr_OnlyTheTableIsUnreachable` (tablonun
>   LİTERAL kopyası, iki yön: her giriş ulaşılamaz VE 43 sınıf × 14 alt koddan kurulan evrende
>   tablo dışındaki her kod ret; ağ, DNS, bağlam, EOF ve bilinmeyen tip satırları) ·
>   `TestReadOperatorRole_AFailureIsNeverUnreachability` (sahte sorgucu rol okumasında
>   `08006`, `57P01`, `28P01`, `53300` döndürür → dördü de ret) ·
>   `TestOperatorDB_AServerThatRefusesStopsTheBoot` (gerçek sunucu, aşağıdaki beş durum + bir
>   kontrol) · `TestOpenOperatorSurface_UnreachableKeepsTheProductUpARefusalStopsTheBoot`'a
>   *"a server that refused: the boot stops"* satırı.
>   **Canlı ölçüm** (gerçek ikili, `:18080`, geliştirme `.env`'i + durumun değişkenleri; her
>   durumda süreç çıktısında sır değeri araması **0**):
>
>   | Durum | Sonuç |
>   |---|---|
>   | operatör değişkeni yok | açılır; `/operator` 503 *not configured*; `/healthz`, `/readyz`, `/` 200 |
>   | kapalı port (ulaşılamaz) | açılır; ERROR `operator_surface=unavailable`, `cannot connect (connection refused)`; `/operator` 503 *unavailable*; müşteri 200 |
>   | G1 müşteri DSN'i + `role=tappa_operator` | açılmaz — `the server refused the connection (SQLSTATE 42501)` |
>   | G2 müşteri DSN'i + `options=-c log_parameter_max_length=-1` | açılmaz — `42501` |
>   | G3 aynı ayar sorgu parametresi olarak | açılmaz — `42501` |
>   | G4 sahibin DSN'i + `role=<olmayan rol>` | açılmaz — `22023` |
>   | G5 sahibin DSN'i + bilinmeyen bir ayar | açılmaz — `42704` |
>
>   Ret satırı yalnız kodu taşır; nöbetçi (olmayan rolün ve bilinmeyen ayarın adı) hiçbir
>   çıktıda yok.
> - **2T-2 · B2** — `### Görevler — A2 tenant-ötesi okuma/yazma` başlığı geri geldi, önünde
>   boş satır; satır HEAD'dekiyle bayt bayt aynı.
> - **2T-3 · B3** — B1'le çelişen yorumlar düzeltildi: `main.go`'nun operatör yüzeyi yorumu,
>   `operator.go`'nun `operatorDialTimeout` ve `openOperatorSurface` yorumları ve süpürmede
>   bulunan `NewOperatorDB` yorumu kapalı listeyi söyler.
> - **2T-4 · B4 — pin harf duyarsız.** pgx bilinmeyen bir sorgu parametresini yazıldığı
>   harflerle başlangıç parametresi yapar; PostgreSQL ayar adını harf duyarsız okur. Ölçüldü
>   (düzeltmeden önce, havuz başına 30 açılış): `?LOG_PARAMETER_MAX_LENGTH_ON_ERROR=-1` müşteri
>   havuzunda **7/30**, operatör havuzunda **9/30**; `?Log_Parameter_Max_Length_On_Error=-1`
>   **10/30** ve **11/30** açılış reddedildi (haritada iki anahtar, Go'nun harita sırası
>   kazananı seçer; geri okuma yakaladığı için sonuç sızıntı değil rastgele RET'ti). Düzeltme:
>   adı `strings.EqualFold` ile eşleyen her anahtar silinir, sonra `"0"` yazılır. Sonra: dört
>   durumda **0/30**. Pinler: `TestPinLogParameters_LeavesOneSpellingOfTheKey` (birim),
>   `TestPin_ADifferentlyCasedKeyCannotUnpinIt` (gerçek sunucu, iki havuz × iki yazım × 30).
>   `logparams.go` yorumu ve ADR 0021 notu buna hizalandı. İlk turun D2 mutantı (*"DSN'de yoksa
>   pinle"*) bu silme döngüsünden sonra EŞDEĞERDİR (anahtar artık hiç bulunmaz) — yerine D2′
>   (*silme tam eşleşmeye döner*) kuruldu, kırmızı.
> - **2T-5 · B5.** (a) PgError yolunda nöbetçi: olmayan rolün ve bilinmeyen ayarın adı
>   rastgele bir nöbetçidir, sunucunun mesajı onu yankılar; hata yalnız `SQLSTATE xxxxx`
>   taşır (`TestOperatorDB_AServerThatRefusesStopsTheBoot`). (b) Geri okuma yalnız tam `"0"`
>   metnini kabul eder; `"-1"`, `"64B"`, `"1"`, `"1B"`, `"1kB"`, `""`, `"0B"`, `" 0"`, `"0 "`,
>   `"00"`, `"-0"` ret (`TestLogParameterPinned_OnlyTheTextZero`); gerçek sunucuda değer `64`
>   ve `1` iken iki havuz da reddeder (`TestPin_ASizeIsRefusedLikeUnlimited`). (c)
>   `OperatorSurfaceConfigured`'ın "HERHANGİ bir alan" okuması pinlendi
>   (`TestOperatorSurfaceConfigured_AnyOneFieldCounts`; komut tarafında
>   `TestOpenOperatorSurface_APartialStructIsNeverSilentlyOff`: tek alanı dolu elle kurulmuş
>   bir yapı asla "off" satırı yazmaz). (d) B1'de (`readOperatorRole`). (e) Çerez muafiyetinin
>   dosya bağı: `TestCookieScanNonCookies_AreBoundToTheirFile` (literal kendi dosyasında muaf,
>   başka üç dosya adında çerez sayılır).
> - **2T-6 · B6** — runbook doğrulaması `pg_stat_activity` aramasından açılış satırına taşındı:
>   `configured` satırı ancak havuz açılıp rol kapısı ve geri okuma geçtikten sonra yazılır;
>   `pg_stat_activity` kanıt değildir (pgxpool `MinConns` 0, `MaxConnIdleTime` ~30 dk —
>   sağlıklı süreçte de sonradan sıfır bağlantı). Gerekçe README'de.
> - **2T-7 · B7** — README'nin iki cümlesi artık aynı şeyi söyler: `tappa-secrets`'ın tek
>   okuması 3a'daki `kubectl describe`'dır ve yalnız anahtar adını ve bayt boyunu basar.
> - **2T-8 · B8** — host hata metni ölçülene daraltıldı (*"not on the customer product's
>   canonical host"*); kural yalnız `TAPPA_BASE_URL`'in host'unu bilir, ingress'in
>   `www.taptime.mt` ve `tappa.everva.com.tr` host'ları kabul edilir (yorumda adıyla). OP-8
>   devrine md. 9 (viii) eklendi: *iki yönlü host kapısı ingress'in BÜTÜN müşteri host'larını
>   dışlamalı*.
> - **2T-9 · B9 — rol kapısı ADR 0021 §1'in kalanını okur:** `rolcreatedb`, `rolcreaterole`,
>   `rolreplication` (`pg_roles`) ve ters üyelik (`pg_auth_members`'ta rolün bir ÜYESİ);
>   dördü de ret, ret mesajı dördünü adlandırır. Pin:
>   `TestOperatorRoleQuery_SeesTheRestOfADR0021sRole` (her nitelik geri alınan bir savepoint'te
>   verilir ve gönderilen okuyucuyla `tappa_operator` olarak okunur) + doğruluk tablosunun
>   dört yeni satırı. Bedel: aynı sorguda dört sütun.
> - **2T-10 · F1 — KAPSAM GENİŞLEMESİ, gerekçeli: müşteri havuzunun `role=` açığı.** Gerekçe:
>   md. 2'de ölçülen açık aynı mekanizmadır (başlangıç parametresi `role=`), `pool.go` bu
>   görevin diff'inde zaten değişiyordu (T79) ve çare bir sütun + bir yüklemdir (orkestratör
>   kararı: bu turda kapat). Yapılan: `readRole` `session_user`'ı aynı bağlantıda okur,
>   `RoleFacts.Session`; `Privileged()` artık `session_user ≠ current_user`'ı da sayar
>   (`signed_in_as_another_role`). Kapının ORTAM davranışı değişmedi: üretimde ret, geliştirmede
>   uyarı (uyarı satırına `session_user` eklendi). Ölçüldü: geliştirmenin ve CI'nin
>   `DATABASE_URL`'i `tappa_app` olarak, `role=` olmadan girer → `session_user = current_user`,
>   kapı onlar için değişmez; üretimin DSN'i ölçülmedi (`kubectl` yok) — runbook onu
>   `tappa_app` olarak kurar. Canlı: sahibin DSN'i + `role=tappa_app` → üretimde açılmaz
>   (`signed_in_as_another_role=true`), geliştirmede açılır ve uyarır. Pinler:
>   `TestNewRefusesASwitchedSessionInProduction` (üretim reddi + geliştirme açılışı + KONTROL:
>   o bağlantıda `SET ROLE NONE` gerçekten süper kullanıcıya ulaşır), `TestRoleRefusal` ve
>   `Privileged` doğruluk tablosuna satırlar. Md. 10 (S-f) kapandı.
> - **2T-11 · F2 — KAPSAM GENİŞLEMESİ, gerekçeli: `db.New`'un ayrıştırma hatası.** `New`
>   pgx'in ayrıştırma hatasını `%w` ile sarıyordu; pgconn'un redaktörü `?password=` biçimini
>   görmez, yani bozuk bir `DATABASE_URL` parolasını açılışın `fatal` satırına taşırdı.
>   Gerekçe: operatör havuzunun ilk turda aldığı önlemin (D12) aynısı, tek satır. Şimdi sabit
>   bir mesaj. Pin: `TestNew_AnUnparseableDSNCarriesNoPassword` (KONTROL: pgx'in kendi metni
>   nöbetçiyi taşır). Canlı: bozuk müşteri DSN'i → sabit mesaj, sır araması 0.
> - **2T-12 · Mutasyonlar (kopyala-geri-yaz, yalnız scratchpad; her mutasyonun diskte olduğu
>   `diff` ile, geri yüklemesi `shasum` ile doğrulandı; hüküm testin çıkış kodundan).**
>   İlk turun 59'u yeniden koşuldu: **50** olduğu gibi kırmızı; **8**'inin hedef satırı bu turda
>   değişti (D3, D7, D8, D9, D11, D17, D20, D21), yeni satırlarına kuruldu, **8/8 kırmızı**; D2
>   eşdeğer oldu (2T-4), yerine D2′ **kırmızı**. Bu turun yeni denemeleri: **26** (N1–N23 +
>   N8b, N18b, N20b; B1: N1, N2, N3, N18, N18b, N19, N20, N20b, N23 · B4: N5 · B5a: N4 · B5b: N6, N7
>   · B5c: N8, N8b · B5d: N3, N19 · B5e: N9 · B9: N10–N14 · F1 ve iki kapının ortak `Session` alanı: N15, N16, N21, N22 · F2: N17);
>   ilk koşuda 5'i derlenmedi (kullanılmayan import/değişken), derlenen hâlleriyle yeniden
>   koşuldu — **26/26 kırmızı**. Ayrıntı görevin raporunda.
> - **2T-13 · Sayılı sınırlar (2. tur):** (i) kapalı liste bir KARARDIR: sınıf 28'in tamamı
>   (yanlış parola dahil) ulaşılamaz sayılır — geri yükleme gerekçesi (md. 4) bunu ister, bedeli
>   yanlış bir parolanın açılışı durdurmamasıdır (yüzey 503 + ERROR satırı); (ii) tanınmayan
>   bir hata TİPİ (ör. pgconn'un TLS reddi) RET'tir — bilinçli olarak kapalı yönde *(2b: tek
>   denemede doğruydu, birleşik hatada değildi — "2b" satırı)*; (iii)
>   üretim `DATABASE_URL`'inin `role=` taşımadığı ölçülmedi (kubectl yok) — taşıyorsa F1
>   üretimde açılışı durdurur ve `fatal` satırı `signed_in_as_another_role=true` diye adlandırır.
> - **2b (2026-10-01, 2. denetçinin ONAY'ından sonra, bloklamayan bulgular; kapsam
>   genişletilmedi).** (1) **Birleşik bağlantı hatası:** pgx çok host'lu bir DSN'in ve
>   `sslmode=prefer`'in denemelerini `errors.Join` ile birleştirir; `errors.As` ilk eşleşmeyi
>   aldığı için kapalı port + TLS reddi / TLS uyarısı / sertifika reddi *ulaşılamaz*
>   okunuyordu, TLS uyarısı (`*net.OpError`, Op `"remote error"`) tek başına da ağ sayılıyordu.
>   Şimdi `connectAttempts` hatayı denemelerine böler (iç içe join'ler dahil) ve
>   `connectFailure` ancak **her** deneme ulaşılamazsa ulaşılamaz der; karar veren deneme
>   gerekçede *"attempt N of M"* diye adlandırılır. TLS uyarısı (`remote error`/`local
>   error`) ve sertifika reddi kendi gerekçesiyle RET'tir. Sunucu cevabı olmayan ret
>   *"the server refused"* DEMEZ (x509'da reddeden bu taraftır): *"the connection attempt
>   failed (…), which is not one of the failures that count as unreachable"*. Pinler:
>   `TestConnectFailure_EveryAttemptDecides` (16 birleşik ya da tek şekil) ·
>   `TestOperatorDB_AJoinedFailureIsUnreachableOnlyWhenEveryAttemptIs` (127.0.0.1'de sahte
>   sunucular, gerçek pgx: istemci sertifikası isteyen sunucu → TLS uyarısı; kapalı port +
>   TLS'i reddeden sunucu, iki sırayla; güvenilmeyen sertifika + kapalı port → dördü RET;
>   KONTROL iki kapalı port → ulaşılamaz) · `TestOperatorDB_AServerThatRefusesStopsTheBoot`'a
>   *"a closed port, then the customer DSN asking to become tappa_operator"* (gerçek sunucu,
>   42501). **Ölçülen bir SONUÇ, sayılı sınır:** `sslmode=prefer` (ya da `sslmode`'suz DSN)
>   TLS'siz bir sunucuya karşı önce TLS sonra düz dener; yanlış parola *TLS reddi + 28P01*
>   birleşimidir ve artık açılışı DURDURUR (`sslmode=disable` ile aynı parola `unavailable`;
>   `TestOperatorDB_PreferAgainstAServerWithoutTLSIsARefusal`, geliştirme sunucusu `ssl=off`).
>   Runbook'un DSN'i `sslmode=disable` taşır; README'nin ret listesi bunu söyler. *(2d:
>   ölçülenden dardı — yalnız yanlış parola değil, listedeki HER sunucu cevabı (3D000
>   ölçüldü; 57P03 ve 53300'ü kapanış denetçisi ölçtü) bu biçimde RET olur; ulaşılamaz kalan
>   yalnız HER denemede ağ düzeyinde olan hatadır, ör. kapalı port.)* (2)
>   **Geri okumanın sorgu hatası dalı:** sunucu hatası artık tipli bir RET
>   (`logParameterReadError`, yalnız SQLSTATE); operatör havuzu onu olduğu gibi geçirir.
>   `TestPin_AReadBackThatCannotRunIsRefused`: kanca bağlantıyı iptal edilmiş bir işlemde
>   bırakır → iki havuz da `25P02` ile reddeder. Sorgu `pg_catalog.current_setting`
>   (`TestReadLogParameterSQL_IsSchemaQualified`). (3) **Boşluktan ibaret DSN:** `config.Load`
>   reddeder (*"is set but holds only whitespace"*); "blanks are a value" alt testleri dört
>   değişkenin her biri için (DSN iki biçimde) *(2d: ayrı ve adı içeriğini söyleyen teste
>   taşındı: `TestLoad_OperatorVariablesOfBlanksAreRefused`)*. Canlı: `" "` ve `" \t\n "` → `fatal`, süreç
>   açılmadı, sır araması 0. (4) **Kardeş cümleler:** ADR 0021 §1 ve §5 (parola Secret'tan
>   değil, `\password` ile stdin'den; üstü çizili + tarihli not), OP-5 md. 15 (ileriye
>   işaret), `20-app.yaml` başlangıç bütçesi (iki dial, ikincisi çıkmaz; ölçüldü: TCP'yi kabul
>   edip hiç cevap vermeyen bir operatör veritabanıyla süreç 10,4 sn'de dinliyor, `/operator`
>   503, `/` 200; en kötü ~20 sn < 60 sn), ADR 0021 §4 *"iki yönde de gürültülü"* ölçülene
>   daraltıldı: `DATABASE_URL` → `tappa_operator` iken müşteri kapısının dört olgusu dördü de
>   `false` (ölçüldü, `SET SESSION AUTHORIZATION`, geri alınan işlem) ve `tenants` `SELECT`'i
>   *permission denied* (ölçüldü); sürecin açılıp ilk istekte düşmesi kod çıkarımı. **Backlog
>   adayı (eklenmedi; 2c md. 3: orkestratörün backlog'una):** üretimde müşteri havuzu `current_user = tappa_operator` (ya da
>   `≠ tappa_app`) iken açılmayı reddetsin. (5) **Runbook:** geri alma 3b yolunda
>   `ExternalSecret` girdilerini kaldırma + `force-sync` + ad sayımı (external-secrets
>   belgesine dayanır, ölçülmedi); 3b'de her `pbcopy`'den hemen sonra `pbcopy </dev/null`
>   ve gerekçesi. **Mutasyonlar (kopyala-geri-yaz, scratchpad):** 17 yeni deneme; 2'si ilk
>   koşuda derlenmedi (kullanılmayan değişken/import), derlenen hâlleriyle kırmızı → **16
>   kırmızı**, **M44 (denetçinin mutantı, yeni kodda) YEŞİL ve EŞDEĞER**: bölmeden sonra her
>   deneme tek bir doğrusal sarma zinciridir, `*pgconn.PgError` de `syscall.Errno` da zincirin
>   UCUDUR (ikisi de sarmaz), yani bir zincir en çok birini taşır ve dalların sırası cevabı
>   değiştiremez. M44'ün tehdit ettiği özellik (kapalı port + 42501 = ret) M44′ (bölme yok),
>   M44″ (bölme yok + errno önce) ve J1 (bir ulaşılamaz deneme yeter) ile kırmızı. 1. ve 2.
>   turdan örnek N1, N2, N3, N5, N15, N17, D1, D6: **8/8 kırmızı**.
> - **2c (güvenlik denetimi, 2026-10-01; `tappa-security-auditor` RED: bir YÜKSEK, dört
>   DÜŞÜK; kapsam genişletilmedi).** (1) **[YÜKSEK] DSN'deki `default_query_exec_mode`
>   log korumasını atlatıyordu.** pgx bu parametreyi kendisi okur, sunucuya göndermez;
>   `simple_protocol`'de argümanları SQL metnine istemci tarafında gömer. Denetçi ölçtü: iki
>   havuz açıldı, pin "0", nöbetçi `current_query()`'de — üretimde hata veren her ifadenin
>   STATEMENT satırıyla pod log'una (§7). Çare: iki kurucu `pinLogParameters`'tan önce
>   `requireBoundParameters` çağırır; kip `boundParameterModes`'ta değilse sabit bir mesajla
>   RET (normalleştirme yok). Ölçüldü, her koşuda
>   (`TestQueryExecModes_OnlyTheListedOnesBindOnTheServer`): beş kipten yalnız
>   `simple_protocol` argümanı metne koyar; `cache_statement`, `cache_describe`,
>   `describe_exec`, `exec` koymaz — liste literal olarak pinli. Harf duyarlılığı ölçüldü:
>   pgx anahtarı da değeri de tam yazımla okur; büyük harfli anahtar pgx'in değildir ve
>   sunucuya bilinmeyen ayar olarak gider (42704, ret), büyük harfli değer ayrıştırılmaz
>   (ret) — kural ayrıştırılmış kipi denetlediği için yazım farkı bir yol açmaz.
>   `TestPin_ADSNCannotChooseClientSideInterpolation` (iki havuz × üç yazım → ret; izinli
>   dört kip açar; KONTROL: ham bağlantıda `simple_protocol` nöbetçiyi metne koyar).
>   Çağrı başına argüman yolu: ~~ürün Go dosyalarında `QueryExecModeSimpleProtocol` seçicisi
>   yok; AST, her import adıyla; pozitif kontrollü; bugün 0 isabet~~ → *2d: ada bağlı tarama
>   dört yazımı görmüyordu; yerine tipe bağlı kural geldi — "2d" satırı* *(2f: kural değil,
>   TUZAK TELİ; çağrı başına kip için tamlık iddiası yok — "2f" satırı)*. Canlı (`:18080`): iki DSN'de de `fatal`, *"asks for
>   default_query_exec_mode=simple_protocol"*, sır araması 0. Md. 10 (S-b) ölçülene eşitlendi;
>   T79 ve ADR 0021 (1)–(2) notları buna bağlandı. (2) **[DÜŞÜK] Rol kapısı sorguları
>   nitelenmemişti.** Ölçüldü (geri alınan işlem, sahip): `search_path = gölge, pg_catalog`
>   ile gölge `=` (name, oid, "char"), gölge `<>` (dolayısıyla `NOT IN`) ve gölge
>   `pg_has_role` öncelik aldı — yani yalnız adlar değil operatörler de. İki sorguda her
>   katalog adı ve fonksiyon `pg_catalog.` ile, her karşılaştırma `OPERATOR(pg_catalog.=)`
>   ile, IN listeleri `= ANY` + `pg_catalog.name[]` ile yazıldı. Pinler:
>   `TestRoleGateQueries_AreSchemaQualified` (metin) ve
>   `TestRoleGateQueries_IgnoreAShadowCatalog` (denetçinin gölge şeması — görünümler,
>   fonksiyon, `oid` üzerinde `=` — sevk edilen iki sorgu gölgesiz okuduğunu okur; KONTROL:
>   aynı metnin nitelenmemiş kopyası gölgeyi okur). (3) **[orkestratörün backlog'una]**
>   üretimde müşteri havuzu `current_user ≠ tappa_app` iken açılmayı reddetsin (2b satırındaki
>   aday). (4) **[DÜŞÜK] Kapalı/ulaşılamaz yüzeyin 503'ü 5xx alarmını çaldırabiliyordu.**
>   5. kuralın sorgusu `status >= 500`'e bakar (ölçüldü: README), yani INFO'ya indirmek
>   yetmezdi; emsale uyan yol seçildi — tasarlanmış cevap kaydedilmez (`probeDesignedStatus`
>   emsali). Tasarım rota kalıbına değil yüzeyin durumuna bağlı olduğu için (aynı kalıplar
>   OP-8'de gerçek handler'ları taşıyacak; `httpx` operatör paketini import edemez) tablo
>   yerine handler'ın bildirimi: `httpx.AnswerAsDesigned(r, 503)`; bildirilenden farklı bir
>   durum (paniğin 500'ü) her rotadaki gibi ERROR kaydı. Durum görünür kalır: açılış satırı
>   (kapalı INFO, ulaşılamaz ERROR). Pinler: `TestAccessLog_ADeclaredDesignedAnswerIsNotAnEvent`,
>   `TestAnswerAsDesigned_OutsideAccessLogIsANoOp`, `TestSurface_ItsDesigned503IsNotAnAlertEvent`
>   (kapalı ve ulaşılamaz: 5 yol × 5 yöntem → 0 kayıt; KONTROL: `/admin` bir INFO kaydı,
>   yapılandırılmış yüzeyin 404'ü kaydedilir). Canlı: kapalı yüzeye 8 istek → `/operator`'da 0
>   kayıt, `/` 1 kayıt. Md. 9 (iii) kapandı. (5) **[orkestratörün backlog'una]** T80'in
>   kapsamına iki DSN alanı (`DatabaseURL`, `OperatorDatabaseURL`). **Mutasyonlar
>   (kopyala-geri-yaz, scratchpad): 16 yeni deneme, 16/16 kırmızı** — ret kaldırıldı (iki
>   havuz ayrı ayrı), her kip kabul, `simple_protocol` listeye eklendi, `exec` listeden
>   çıktı, ürün koduna kip argümanı eklendi, tarama hiçbir dosyayı okumadı, tarama hiçbir şey
>   aramadı; dört nitelik geri alma (`pg_roles`, bir `=`, `pg_has_role`, `NOT IN`); dört
>   erişim log'u mutantı (yüzey bildirmez, log bildirimi yok sayar, bildirim her durumu
>   susturur, bildirim boş). "Yalnız tam yazımın reddi" mutantı UYGULANAMAZ: kural yazımı
>   değil pgx'in ayrıştırdığı kipi denetler ve pgx yalnız tam yazımı okur (ölçüldü). 2b'den
>   örnek J1, J3, M38, W1: **4/4 kırmızı**.
> - **2d (kapanış denetimi, 2026-10-01; RED: bir bloklayıcı, dört bloklamayan; kapsam
>   genişletilmedi).** (1) **[BLOKLAYICI] Çağrı başına kip yasağı ada değil TİPE bağlandı.**
>   2c'nin taraması yalnız `QueryExecModeSimpleProtocol` adlı seçiciyi görüyordu; denetçi dört
>   yazımı geçirdi (dot import, `pgx.QueryExecMode(5)`, `pgx.QueryExecModeExec + 1`,
>   `…DefaultQueryExecMode = 5`) ve sevk edilen müşteri havuzunda çağrı başına
>   `pgx.QueryExecMode(5)` nöbetçiyi `current_query()`'ye koydu. ~~Kapalı kural:~~ *(2f: tuzak
>   teli, tamlık iddiası yok — "2f" satırı)* `TestProductCode_ExecModeWireAndConnectWire`
>   *(2f'de yeniden adlandırıldı)* modülün bütün ürün paketlerini
>   (`go list -export -deps github.com/atknatk/tappa/...`; **34 paket, 207 test dışı Go
>   dosyası** — ~~modülde yapı kısıtıyla dışarıda kalan tek dosyalar iki `_test.go`, ölçüldü~~
>   *2e: üç, ve hepsi `_test.go` — `adminauth/timingsamples_*`, `db/race_*`,
>   `operatorauth/race_*`; sayı artık yazılmıyor, test hesaplıyor*)
>   `go/types` ile bağımlılıkların tam export verisine karşı denetler (OP-6'nın `exactImports`
>   emsali: sürüm koruması, `-race` eşlemesi `race_on_test.go`/`race_off_test.go`, her hata
>   kırmızı). Tipi `pgx.QueryExecMode` olan her ifade (sabit, dönüşüm, aritmetik, son tipini
>   almış untyped sabit, tip ifadesi, alan okuması) ve `DefaultQueryExecMode` alanının her
>   kullanımı, `internal/db/logparams.go`'nun `boundParameterModes` ve
>   `requireBoundParameters` bildirimleri dışında kırmızı *(2i: "bildirimleri" = PAKET DÜZEYİ
>   bildirimler, dosyanın kendi konumuyla; aynı adlı yöntem ve `//line` yönergesi izinli
>   sayılmaz; izinli kullanım tam olarak 16 — "2i" satırı)*. Pozitif kontroller: dokuz yazım
>   (denetçinin dördü, ad, generic örnek, alias, `any`'den tip iddiası — S1 emsali —,
>   literal anahtarı) her biri bulunuyor; `logparams.go`'daki izinli kullanımlar sayılıyor
>   (9). Süre: 0,95 sn (`-race` 2,6 sn). ~~**Kalan tek sınır, adıyla: reflection ya da
>   `unsafe`** (ölçüldü, mutasyonlarda).~~ *2e: ölçülenden genişti — "2e" satırı.* ADR 0021 notu ve 2c satırı hizalandı. (2)
>   **`AnswerAsDesigned`'ın iki sınırı pinlendi:** aynı router'da art arda
>   `TestAccessLog_ADeclarationIsThisRequestsAndThisStatusOnly` — bildirilmiş 503'ten sonra
>   bildirilmemiş 503 ERROR, bildirilmiş 503'ün yerine 502 ERROR. `AccessLog` yorumu
>   güncellendi. (3) README sınır **32**: kapalı/ulaşılamaz yüzeyde `/operator` denemeleri
>   süreç log'unda iz bırakmaz; "kayıt yok" ile "istek yok" ayırt edilemez; tarama yalnız
>   ingress log'unda görünür; `unavailable` ERROR satırını okuyan kural yok — 6. kural
>   emsalinde öneri, OP-8'e devir (md. 9 (ix)). (4) `sslmode=prefer` sınırı ölçülene
>   genişletildi: listedeki her sunucu cevabı ret (3D000 eklendi,
>   `TestOperatorDB_PreferAgainstAServerWithoutTLSIsARefusal`; kapalı port ulaşılamaz kalır).
>   (5) Çok denemeli ulaşılamaz mesaj her denemenin kodunu sırayla verir (*"attempt 1:
>   connection refused; attempt 2: SQLSTATE 28P01"*;
>   `TestConnectFailure_AnUnreachableJoinNamesEveryAttempt`). Not: boşluk alt testleri
>   `TestLoad_OperatorVariablesOfBlanksAreRefused`'a taşındı. **Mutasyonlar
>   (kopyala-geri-yaz; kaçışlar YENİ bir ürün paketinde, `internal/zzescape`, koşudan sonra
>   silindi): 17 deneme.** Kırmızı 15: denetçinin dört kaçışı (dot import, dönüşüm,
>   aritmetik, alana untyped sabit), ad kontrolü (`pgx.QueryExecModeSimpleProtocol`), generic
>   örnek, alias, `any`'den tip iddiası (S1), literal anahtarı, `logparams.go`'da izinli iki
>   bildirimin dışında bir kullanım, tarama tipi hiç tanımıyor, modül yürüyüşü paket
>   tutmuyor; B3 (bildirim 500 dışı her durumu susturur), B4 (yuva kurulum başına), çok
>   denemeli mesaj yalnız ilk denemeyi söyler. **YEŞİL 2, beklenen ve adıyla sayılan
>   sınır:** reflection (`reflect.Value.FieldByName(...).SetInt`) ve yansımayla bulunmuş
>   ofsetle `unsafe` yazımı — bu tipte bir ifade yazmadan değeri yazan iki yol. Önceki
>   turlardan örnek E1, Q2 (2c), J1, M38 (2b): **4/4 kırmızı**.
> - **2e (2. kapanış denetimi, 2026-10-01; RED: bir bloklayıcı, dört bloklamayan; kapsam
>   genişletilmedi).** Sınıf iki turdur aynıydı — kip yasağının yazılı iddiası ölçülenden
>   genişti — bu yüzden **katman değişti: metin denetiminin yanında davranış ölçülüyor.**
>   (B-1) **[BLOKLAYICI] Build kısıtlı ürün dosyası taramadan kaçıyordu:** `!race`/`!cgo`
>   etiketli bir dosya CI biçiminde (-race, cgo açık) yoktu, üretimde (CGO_ENABLED=0, -race
>   yok) vardı. (a) `go list` artık `IgnoredGoFiles`, `CgoFiles`, `IgnoredOtherFiles` da
>   istiyor; test dışı her girdi kırmızı. (b) Ortamdan bağımsız kural
>   `TestProductCode_CarriesNoBuildConstraint`: ürün `.go` dosyası `//go:build` / `// +build`
>   satırı, GOOS/GOARCH dosya adı eki (`go tool dist list`'ten), `import "C"` ya da go'nun yok
>   saydığı ad (`_`, `.`) taşımaz. Ölçüldü: bugün böyle ürün dosyası **yok** (207 dosya
>   okundu), izinli liste boş. (N-1) **Kurucunun içinde kontrolden sonra kip değişimi:**
>   `pinLogParameters`'ın `AfterConnect`'i her yeni bağlantıda bağlantının KENDİ kipini
>   (`c.Config()`) `requireBoundParameters` ile denetler; ret tipli
>   (`execModeRefusedError`), operatör havuzu onu olduğu gibi geçirir. Kurucuların erken
>   kontrolü kaldı ve "bağlanmadan önce" olduğu pinlendi (kapalı port + `simple_protocol` →
>   kip reddi, bağlantı hatası değil). **Davranış testi**
>   `TestPools_KeepArgumentsOutOfTheStatementText`: iki kurucunun döndürdüğü havuzda üç ayrı
>   bağlantı tutulur, her birinde nöbetçi argüman `current_query()`'de YOK; kontrol:
>   `simple_protocol` nöbetçiyi koyar. **Operatör havuzu test kancasıyla** (`openOperatorDB` +
>   `asOperator`) — ölçerek seçildi: `tappa_operator` dev'de NOLOGIN; `BEGIN … ROLLBACK`
>   içinde verilen parola commit edilmediği için yeni bir bağlantıya görünmez; commit edilen
>   bir giriş, koşu yarıda ölürse kalıcı iz bırakır; ölçülen özellik pgx'in istemci
>   tarafıdır ve hangi rolün girdiğinden bağımsızdır. `TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection`:
>   (a) havuz kurulmadan önce, (b) ilk bağlantıdan sonra havuzun config'inde değişen kip,
>   bağlantısında reddedilir. (N-2) Taranan paket kümesi `go list github.com/atknatk/tappa/...`
>   ile **birebir** eşit (sıralı karşılaştırma). (N-3) Eşzamanlı alt test
>   `TestAccessLog_ADeclarationIsThisRequestsWhileAnotherRuns`: iki sırayla iki istek üst
>   üste biner; her seferinde tam bir kayıt, bildirimsizinki. (N-4) Sayı düzeltildi; artık
>   yazılmıyor. ~~**Sayılı sınırlar, adıyla:** çağrı başına reflection ya da `unsafe` ile
>   yazılmış bir kip (havuz çapında olanı bağlantı katmanı ve davranış testi yakalar); kod
>   içinde SQL kurmak (`QueryRewriter`, `fmt.Sprintf`) bu kuralın konusu değil (CLAUDE.md §6;
>   `operator.go`'nun bağlı-parametre pini).~~ *(2f: bu "kalan" listesi de geniş çıktı; çağrı
>   başına kip için tamlık iddiası kaldırıldı — "2f" satırı)* **Mutasyonlar (kopyala-geri-yaz; prob
>   dosyaları koşudan sonra silindi):** 19 deneme, **19/19 kırmızı** — B-1 probları
>   (`!race` düz ve `-race` koşuda, `!cgo`, `_linux.go`, eski `// +build`, `//go:build
>   ignore`, cgo dosyası, teste benzeyen ad `zz_tests.go`, go'nun yok saydığı `_zz.go`,
>   `cmd/tappa`'ya prob), W1, E2a-op, E2a-cust, X13, X13b (kontrol her şeyi kabul edip
>   `simple_protocol` yazar — yalnız davranış testi ve bağlantı katmanı görür), L1 (bağlantı
>   katmanı yok + kurucuda takas), L2/L2b (ilk bağlantıdan sonra kip değişir, bağlantı
>   katmanıyla ve onsuz), B4b (`-race`). Örnekler: 2d X2, X4, X6 ve 2c Q2 kırmızı; 2c E1 ilk
>   koşuda YEŞİL kaldı — bağlantı katmanı aynı reddi ping'de verdiği için — erken reddin
>   bağlanmadan önce olduğunu pinleyen satır eklendi, E1 ve E2 yeniden: kırmızı.
> - **2f (3. kapanış denetimi, 2026-10-01; RED; aynı sınıfın ÜÇÜNCÜ turu — kip yasağının
>   yazılı iddiası ölçülenden genişti).** Kaçış: `pgxtest.AllQueryExecModes` + çıkarımlı bir
>   generic (`func last[S ~[]E, E any](s S) any`) çağrı başına `simple_protocol` seçti;
>   reflection yok, `unsafe` yok, kip tipinde bir ifade yazılmadı; sevk edilen müşteri
>   havuzunda nöbetçi `current_query()`'de ölçüldü, bütün testler yeşildi. **Orkestratör
>   kararı — agent-brief M8-02 FAZ C dersi: "sınıfı kıran şey iddianın biçimidir"; üçüncü
>   kez yama değil, İDDİANIN BİÇİMİ değişti:** (1) **Havuz düzeyi, aynen kalır** *(2h: "KAPALI" sözcüğü
>   kaldırıldı; cümle bugün sevk edilen kod hakkında, ölçülen — "2h" satırı)*:
>   sevk edilen iki havuzun hiçbir bağlantısı argümanı metne gömen bir varsayılan kiple
>   çalışmaz (DSN denetimi + bağlantı başına denetim + davranış testi) *(2g: taşıyıcılarının
>   yakaladığı ölçülenden geniş yazılmıştı — "2g" satırı; ölçülene eşit cümle orada)*. (2) **Çağrı başına
>   kip için tamlık iddiası YOKTUR**; kod içinde SQL kurmakla aynı sınıf, taşıyan kurallar
>   CLAUDE.md §6 ve `operator.go`'nun bağlı-parametre pini. Tip taraması bir **tuzak teli**:
>   `TestProductCode_ExecModeWireAndConnectWire` (yeniden adlandırıldı) yalnız listelediği
>   yazımları yakalar — `execModeEscapes`: dot import, dönüşüm, aritmetik, alana untyped
>   sabit, adlı sabit, generic örnek, alias, `any`'den tip iddiası, literal anahtarı,
>   `pgxtest` listesi + çıkarımlı generic, işlev değeri; yakalamadığı her yol (örnek:
>   bilmediği bir kaynaktan beslenen çıkarımlı generic, reflection, `unsafe`,
>   `QueryRewriter`/`fmt.Sprintf`) kod incelemesinin ve §6'nın konusudur. Tamlık cümleleri
>   `logparams.go`'dan, test dosyasından, ADR 0021'den, bu kartın 2c/2d/2e satırlarından ve
>   md. 10'dan kaldırıldı (üstü çizili + 2f notu). (3) **Yeni KAPALI yapısal kural:** üretim
>   ikilisinin bağımlılık kapanışı `testing`'i (ve `testing/...`'i) ve `pgxtest`'i içermez —
>   `cmd/tappa`'da `TestBinary_LinksNoTestCode` (`go list -deps`, `CGO_ENABLED=0`, üretimin
>   build'i; pozitif kontrol: `pgxtest`'in kendi kapanışında `testing` bulunuyor). Önce
>   ölçüldü: bugün ikisi de kapanışta YOK (CGO açık ve kapalı). (4) **Tel ucuzca
>   güçlendirildi, iddia büyütülmeden:** `carriesExecMode` işaretçi, dilim, dizi, map, kanal,
>   imza, demet ve generic adlı tipin tip argümanlarına bakar; `info.Instances`'ın tip
>   argümanları ve tanımlanan nesnelerin tipleri de denetlenir. (N-1) Build kısıtı kuralı
>   ölçülene eşitlendi: ad go/build gibi ayrıştırılır (ilk `.`'dan kesilir, ilk `_`'den
>   öncesi atılır, sondaki `_test` atılır), import yolu `strconv.Unquote` ile çözülür,
>   GOOS/GOARCH listesi çalışan toolchain'in `internal/syslist`'inden (go/build'in kullandığı
>   liste; zos, hurd, nacl, arm64be dahil) türetilir; kapsadıkları test yorumunda adıyla,
>   kapsamadığı "listede olmayan başka bir dışlama yolu". (N-2) **Kaynak pini**
>   `TestConstructors_TheHookReachesOnlyThePin`: `New` tam olarak `return newDB(ctx, cfg,
>   nil)`, `NewOperatorDB` tam olarak `return openOperatorDB(ctx, cfg.OperatorDatabaseURL,
>   nil)`; iki kurucuda `before` yalnız `pinLogParameters`'ın üçüncü argümanıdır ve o çağrı
>   gövdenin kendi deyimidir (koşul altında değil). (N-3) Aynı tip taramasına bir **tel**:
>   `pgx.Connect*`, `pgxpool.New`/`NewWithConfig`, `pgconn.Connect*`/`Construct` iki kurucu
>   (`newDB`, `openOperatorDB`) dışında ~~hiçbir ürün dosyasında kullanılamaz~~ *(2h: telin
>   gördüğü kullanımlarda kırmızı)* (değer olarak da); bu da bir tel, tamlık iddiası değil. (N-4) Bağlantı katmanının reddi artık kipi DSN'e
>   yüklemiyor: *"the connection's configuration (the … pool) is in query exec mode …"*;
>   DSN'den geldiyse *"asks for default_query_exec_mode=…"*; README'nin ret listesi ikisini
>   de söylüyor. **Mutasyonlar (kopyala-geri-yaz; prob dosyaları silindi): 16 yeni deneme,
>   16/16 kırmızı** — R3a (`pgxtest` importu, ikilinin kapanışındaki bir ürün paketinde),
>   R3b (`testing`), R3c (`testing/iotest`); M4 (yerel `[]pgx.QueryExecMode` + çıkarımlı
>   generic, `pgxtest`'siz), M4b (denetçinin kaçışı, ürün dosyasında), M4c (kip anahtarlı bir
>   map, tip yazılmadan); N-1 probları (`zzmode_linux.impl.go`, ters tırnaklı ham dizgeyle `C` importu,
>   `zz_zos.go`, yalnız `a_arm64.impl.go` taşıyan yeni paket); D1, D2, D3 (`NewOperatorDB`
>   DSN'e parametre ekler); N3a (`pgx.Connect`), N3b (`cmd/tappa`'da `pgxpool.New` değeri);
>   N4 (bağlantı katmanı DSN'i suçlar). Önceki örnekler E1, E2 (2c), L1, L2, P1r, W1 (2e):
>   **6/6 kırmızı**.
> - **2g (4. kapanış denetimi, 2026-10-01; RED: bir bloklayıcı, bir ucuz; kapsam
>   genişletilmedi).** (B-1) **Havuz düzeyi dört cümle ölçülenden genişti** (`logparams.go`,
>   ADR 0021, `operatorrefusal_test.go`, `execmodetypes_test.go`): MP1 (`newDB`'de pin'den
>   sonra `if cfg.IsProd() { poolCfg, _ = pgxpool.ParseConfig(dsn+"…simple_protocol") }` —
>   pin'in `AfterConnect`'ini de götürür), MP2 (aynısı `openOperatorDB`'de kullanıcı adına
>   bağlı), K4 (yalnız üretimde `AfterConnect = nil`) bütün suiti yeşil bıraktı; bugünkü kod
>   düz çizgi ve iddia bugün doğru, ama taşıyıcı testlerin yakaladığı ölçülenden geniş
>   yazılmıştı. **(a) Kaynak pini — ~~değişiklik bir karar olsun~~** *(2h: pinli
>   bildirimlerin TOKEN değişikliği testi kırmızı yapar; bildirimlerin dışı pinli değil)*:
>   `TestConstructors_BodiesAreTheReviewedOnes` `newDB`, `openOperatorDB`,
>   `pinLogParameters` ve `requireBoundParameters`'ın gövdesini `format.Node` ile basar ve
>   test dosyasındaki literal metinle **token token** karşılaştırır (yorumlar, boş satırlar,
>   satır düzeni yok sayılır; her noktalı virgül tek yazımla). Ölçüldü: gövdeye yorum
>   eklemek ve iki satırlık birleştirmeyi tek satıra almak YEŞİL; yerel değişken yeniden
>   adlandırmak, deyim taşımak, hata metni değiştirmek, deyim eklemek KIRMIZI; kapanışı yeni
>   satıra alan ve sondaki virgülü ekleyen bir yeniden akış da KIRMIZI (bir token eklenir —
>   sayıldı). Kırmızı mesajı değiştirenin neyi yeniden doğrulaması gerektiğini adıyla söyler:
>   davranış testi (müşteri havuzu dev ve üretim ortamıyla), bağlantı başına denetim, ADR 0021
>   notu. Ek tel: `pgxpool.Config`'in `AfterConnect`, `BeforeConnect`, `ConnConfig`
>   alanlarına `pinLogParameters` dışında yazmak (atama, artırma, adres alma, literal
>   anahtarı) kırmızı; bugün izinli tek yazım `pinLogParameters`'taki `AfterConnect`. **(b)
>   Dört cümle ölçülene daraltıldı:** "bugünkü kurucular düz çizgidir ve kaynakları token
>   token pinlidir; `pinLogParameters`'ın `AfterConnect`'i havuzun kancası olarak kaldıkça,
>   havuzun config'inde sonradan yapılan bir kip değişikliği bağlantıda reddedilir" —
>   "ulaşamaz" gibi koşulsuz ifadeler kalktı; 2f satırına ve md. 10'a 2g notu eklendi. **(c)
>   Davranış testi üretim ortamıyla da:** `TestPools_KeepArgumentsOutOfTheStatementText`
>   müşteri havuzunu `Env=dev` ve `Env=prod` ile koşar (dev'in `tappa_app` rolü üretim rol
>   kapısından geçer — ölçüldü, test yeşil); operatör havuzunda üretim yolu ile test kancası
>   yolu arasındaki tek farkın — *2h: pinli bildirimlerin içinde* — `before` olduğu iki
>   kaynak pininde görünür ve test yorumunda yazılı. (N-1) `TestBinary_LinksNoTestCode` artık `GOOS=linux GOARCH=amd64` ile ölçer:
>   imaj deploy iş akışının `ubuntu-latest` koşucusunda `docker build` ile, `--platform` ve
>   GOOS/GOARCH olmadan, golang bookworm imajında kurulur (Dockerfile'ın kendi cümlesi:
>   "linux/x64") — makinenin GOOS/GOARCH'ı değil. **Mutasyonlar (kopyala-geri-yaz):** MP1,
>   MP2, K4, K4b (`openOperatorDB`'de `AfterConnect = nil`) — **4/4 kırmızı** (MP1'i davranış
>   testi de üretim ortamında yakalar). Kaynak pinine karşı öz-denetim 9 deneme: yorum ekleme
>   ve salt düzen (birleştirilen satır + boş satırlar) YEŞİL — beklenen; yerel değişken
>   yeniden adı, deyim taşıma, hata metni, `pinLogParameters`'a deyim ekleme,
>   `requireBoundParameters`'a bir kip daha, sondaki virgüllü yeniden akış KIRMIZI;
>   `BeforeConnect`'e yazan yeni bir ürün dosyası tel ile KIRMIZI. Önceki örnekler E1, E2
>   (2c), L1, L2 (2e), R3a, M4 (2f): **6/6 kırmızı**.
> - **2h (5. kapanış denetimi, 2026-10-01; RED; aynı sınıfın BEŞİNCİ kapanış turu).**
>   Sevk edilen kodda mutasyonsuz kaçış bulunmadı (denetçi 20 DSN/ortam varyantı denedi);
>   bulgular metindi: `boundParameterModes`'in başlatıcısı, importlar ve
>   `requireLogParametersPinned`/`logParameterPinned` token pininin dışındaydı (B6a: liste
>   üretimde `simple_protocol` ekleyen bir çağrı; B6b: `slices` importu yönlendirilir; B6c:
>   üretimde `return nil`), pinsiz `readRole` üretimde reflect+`unsafe` ile `afterConnect`'i
>   sıfırlayabiliyordu (R1), havuz-config teli bazı yazım biçimlerini görmüyordu, ve metinler
>   bunları kapsıyormuş gibi konuşuyordu. **Orkestratör kararı — iddianın biçimi son kez ve
>   kalıcı olarak (agent-brief M8-02 FAZ C; "hükmü kaldır"):** kök, metnin gelecekteki KEYFİ
>   kod değişikliklerine karşı bir garanti gibi okunmasıydı; hiçbir test bunu kanıtlayamaz.
>   Bütün metinler (`logparams.go`, `execmodetypes_test.go`, `operatorrefusal_test.go`,
>   `testcode_test.go`, ADR 0021, bu kartın 2c–2g satırları ve md. 10) artık YALNIZ üç şey
>   söyler — kanonik metin `logparams.go`'da `boundParameterModes`'in yorumunda, Türkçesi ADR
>   0021'in 2h notunda: **(I) bugün sevk edilen kod, ölçülen davranış** — iki havuzun her
>   bağlantısı bağlı parametre gönderen bir kipte, log parametresi 0'a pinli; ölçen testler
>   adıyla; **(II) adıyla sayılan pinler ve teller, neyi yakaladıkları tam liste** — gövde
>   pini (`newDB`, `openOperatorDB`, `pinLogParameters`, `requireBoundParameters` gövdeleri +
>   `boundParameterModes` başlatıcısı), kanca pini, KİP/BAĞLANTI/HAVUZ-CONFIG telleri ve
>   listeledikleri biçimler, kapalı yapısal kurallar (ikili kapanışı linux/amd64 cgo=0; build
>   kısıtı türleri); **(III) tamlık iddiası yok** — listelenmeyen her değişiklik (pinsiz
>   yardımcılar, importlar, başka başlatıcılar, ortama koşullu davranış, reflect, `unsafe`,
>   çağrı başına kip) kod incelemesinindir. Bu karar OP-8 ve sonrası için de geçerli (md. 9
>   (x)). **Ucuz eklemeler:** `boundParameterModes` başlatıcısı token pinine eklendi (B6a
>   artık kırmızı; değeri karşılaştıran test yorumu buna göre yeniden yazıldı); havuz-config
>   teline `*c.ConnConfig = …`, `for _, c.AfterConnect = range …` ve bütün `pgxpool.Config`
>   değeri (`*c = *d`) eklendi ve listede adıyla — görmediği biçimler de adıyla
>   (`c.ConnConfig.Config = …`, gömülü alan, reflect, `unsafe`); N-2: ikili kuralının cümlesi
>   "üretim ikilisine bağlanan hiçbir paket" oldu, `cmd/rotatekek` kapsam dışı yazılı.
>   **Mutasyonlar:** B6a kırmızı; B6b, B6c ve R1 yeşil — beklenen ve (III)'te sınıfıyla
>   adlandırılmış; yeni tel biçimleri pozitif kontrollerde. Ayrıntı görevin raporunda.
> - **2i (6. kapanış denetimi, 2026-10-01; RED, ama 2h çerçevesi tuttu: PART I'de ihlal yok;
>   kapsam genişletilmedi).** (1) **[BLOKLAYICI, PART II eksiği] KİP telinin izinli bölgesi
>   yalnız ad ve `//line` ile tanınıyordu:** `logparams.go`'da `requireBoundParameters` adlı
>   bir YÖNTEM ve başka bir paketteki `//line ../db/logparams.go:N` arkasındaki aynı adlı
>   fonksiyon izinli sayılıyordu (denetçi: yöntemi çağıran bir ürün fonksiyonuyla nöbetçi
>   operatör havuzunda `current_query()`'ye girdi, suit yeşildi); `allowedModes` bir alt
>   sınırdı. Çare: `enclosingDecl` yöntemi `"(method) <ad>"` diye adlandırır, izinli bölge
>   yalnız PAKET DÜZEYİ bildirim (`allowedHit`); bütün tellerin konumları
>   `PositionFor(pos, false)` — `//line` uygulanmadan; üç sayı da tam eşitlik (16 kip
>   kullanımı — ölçülen —, 2 açıcı, 1 kanca yazımı). İzinli bölge kontrolleri: aynı adlı yöntem
>   ve `//line` arkasındaki fonksiyon telde görünür ve izinli sayılmaz; `//line` kontrolünün
>   boş olmadığı (düzeltilmiş konumun `logparams.go` dediği) ayrıca denetlenir. (2) ADR 0021
>   (i) cümlesi daraltıldı: `o.Exec`/`o.QueryRow` derlenmez (ölçüldü,
>   ~~`TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor`~~ *2j: derleyici "has no field or method
>   Exec" der; pinleyen `TestOperatorDB_EveryMethodDelegatesVerbatim`*); reflect/`unsafe` PART III. (3) Build
>   kısıtı yürüyüşü artık go'nun atladığı dizinlere de girer (`_x`, `.x`, `testdata`; yalnız
>   `.git` ve iç içe modüller — *2j: go.mod DOSYASI taşıyan dizin; sembolik bağlı dizinler
>   izlenmez* — dışarıda; ölçüldü: bugün böyle bir dizinde `.go` dosyası yok);
>   yürüyüş `constrainedProductFiles`'a ayrıldı ve geçici bir ağaçta kontrol edilir. (4)
>   `TestPin_AConnectionTheParameterDidNotReachIsRefused`'a operatör havuzu için "a later
>   connection, operator pool" alt testi (test kancasıyla) eklendi; "her yeni bağlantı" atfı
>   artık iki havuz için ölçülü. (5) `pool.go` ve `operatorpool.go`'daki ölçülmüş olgulara test
>   adları iliştirildi. **Gözlem yakalatıldı:** HAVUZ-CONFIG teli `pgxpool.Config`'in alttaki
>   struct'ını taşıyan her tipin bütün değer yazımını da görür (`type C pgxpool.Config;
>   *(*C)(c) = d`); listede adıyla. **Mutasyonlar (kopyala-geri-yaz): 11 deneme, 11/11
>   kırmızı** — yöntem kaçışı, `//line` kaçışı, sayıyı 15 beklemek, `enclosingDecl`'in alıcıyı
>   yok sayması, düzeltilmiş konum, tanımlı tiple bütün-değer yazımı, okuma yalnız ilk
>   bağlantıda (operatör alt testi kırmızı), yürüyüşün `_` ve `testdata` dizinlerini yeniden
>   atlaması (2), `internal/_zz`'de kısıtlı bir ürün dosyası. Metinler (`logparams.go` PART II,
>   test dosyası başlığı, `poolConfigHooks` yorumu, ADR 0021 2h notu, 2d satırı) telin yaptığına
>   eşitlendi.
> - **2j (7. kapanış denetimi, 2026-10-02; ONAY, iki ucuz bulgu; kapsam genişletilmedi).**
>   (B-1) ADR 0021 (i) ve 2i satırındaki "`o.Exec`/`o.QueryRow` derlenmez" atfı yanlış teste
>   gidiyordu (`TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` iki yöntem eklemeyi
>   yakalamıyor); artık derleyicinin *"has no field or method Exec"* ölçümüne ve yöntem
>   kümesini pinleyen `TestOperatorDB_EveryMethodDelegatesVerbatim`'e gider. (B-2)
>   `constrainedProductFiles` go.mod adlı bir DİZİNİ iç içe modül sanıp o paketi atlıyordu:
>   artık yalnız go.mod DOSYASI (`!fi.IsDir()`); yürüyüş kontrolüne `dirmod/go.mod/` dizini +
>   `dirmod/g_linux.go` satırı eklendi. Sembolik bağlı dizinler izlenmez — kapsam listesinde
>   adıyla; o yoldan gelen ve bir ürün paketince import edilen paket tel testinin paket
>   kümesi eşitliğinde görünür (denetçi). Mutasyon: `!fi.IsDir()`'ı geri almak kırmızı.

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
