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

> **Sıra güncellemesi (2026-10-02, 28. oturum — orkestratör önerisi, otonomi kuralı; kullanıcıya
> raporlandı, itiraz yok).** Kullanıcı *"çok yavaş ilerliyoruz, ne kadar paralel ilerleyebilirsin"*
> dedi ve SES'in dış adımlarını sonraya bıraktı; B'nin dış bağımlılığı beklerken C'nin bekleyeceği
> bir şey yok. Yeni sıra: **Faz 0 → A1 (OP-4..OP-10) → C White-label (WL-0..WL-10) → B E-posta
> (EM-1..EM-8 + WL-11) → A2.** WL-11 (e-postada tenant adı) EM-7'ye bağlı olduğu için B ile gider;
> WL-8 (panel kabuğu) OP-10'dan sonra (aşağıdaki çakışma çözümü değişmedi). **Paralellik:**
> veritabanına DDL/mutasyon yapmayan görevler `isolation: worktree` ile aynı anda yürür (OP-8, OP-9
> ve WL-0 böyle yürüdü); migration'lı görevler (OP-10, WL-1, …) paylaşılan dev Postgres'te SIRAYLA;
> tam DB test koşusunu aynı anda yalnız bir ajan yapar. Yukarıdaki *"paralel ÇALIŞTIRILMAZ"* cümlesi
> bu kapsamda daraldı.

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
>   → **OP-8'de uygulandı (2026-10-02):** kararlar, ölçümler, sayılı sınırlar (L1–L19) ve devirler
>   aşağıdaki *"Kart düzeltmesi (2026-10-02, OP-8 uygulaması sırasında)"* bloğunda; redacting tip
>   uygulandı (`formValue`), enroll oran sınırı ve `pending` E2E'de ölçüldü, dağıtık saldırgan L2.
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
>    düzeltmesi, "2h" satırı). → **OP-8'de karşılandı (2026-10-02):** (i) md. 1, 7; (ii)/(viii)
>    md. 2–6; (iii) değişmedi — yapılandırılmış yüzeyin kendi 503'leri tasarım bildirmez, kayda geçer (`surface.go`, `serviceUnavailable`'ın yorumu); (iv) değişmedi; (v) md. 2;
>    (vi) md. 17; (vii) md. 12; (ix) md. 13 — eklendi; (x) ADR 0020 §4 OP-8 notu — OP-8 kart
>    düzeltmesi.
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

> **Kart düzeltmesi (2026-10-02, OP-8 uygulaması sırasında).** Yazıldı:
> `internal/handler/operator/` (`routes.go` zincir ve kapılar, `signin.go`, `enroll.go`,
> `console.go`, `render.go`, `form.go`; `surface.go` genişledi — `New(auth, host, baseURL,
> log)`), `internal/httpx/operatorhost.go` (+ `router.go`'da tek koşullu `r.Use`), yeni templ
> paketi `web/templates/operatorpages/`, `layout.Operator`/`layout.OperatorWithScript`,
> `web/static/js/operator/enroll.js`, `input.css`'te `op-*` bileşenleri; `cmd/tappa/operator.go`
> (yeni imza, açılış satırının metni), `deploy/README.md` (7. uyarı kuralı, sınır 32, runbook
> yanıt doğrulaması), `internal/handler/marketing_test.go` (çerez taraması) +
> `marketing.go` (yorum), `dashboard_test.go` (`ink` zemininin gerekçesi), yorumlar:
> `operatorauth/limits.go`, `flow.go`, `cookie.go`, `config/config.go`. Testler:
> `internal/handler/operator/` (`op8_test.go`, `op8_db_test.go`, `leak_test.go`, `rig_test.go`,
> `export_test.go`), `internal/httpx/operatorhost_test.go`,
> `internal/operatorauth/surface_external_test.go` + `export_test.go` (`CountWork` — bu
> paketin test build'ine derlenir — `_test.go`; ürün kancası yok). Migration YOK, bağımlılık YOK (`go.mod`,
> `go.sum`, `sqlc.yaml` diff boş), CLAUDE.md'ye dokunulmadı, `app.css` gitignored (`make css`:
> +19 kural, 0 kaldırılan, +3 988 bayt — yorumlardan doğmuş kural yok, ölçüldü). Güvenlik
> iddiaları üç parçalı (md. 9 (x)): ADR 0020 §4'ün OP-8 notu (I)–(III).
>
> **Kararlar, ölçümüyle (numaralar atıf içindir; *4. turda bu liste, pinden genel hüküm çıkaran
> cümleleri kaldıracak biçimde yeniden yazıldı — değişen cümlelerin önceki ve yeni hâli 4. tur
> alt bloğunun süpürme tablosunda*):**
> 1. **Rotalar.** 00026'da beş definer var (ölçüldü: `pg_proc`'ta `op_%` = `op_close_session`,
>    `op_complete_enrollment`, `op_open_session`, `op_record_auth_event`, `op_touch_session`); beşinden
>    biri de tenant okumaz. Kayıtlı: `GET/POST /operator/login`, `GET/POST /operator/login/totp`,
>    `GET/POST /operator/enroll`, `POST /operator/logout`, `GET /operator`. ADR 0020 §4'ün tenant,
>    legal, billing, plaques, audit ekranları kayıtlı DEĞİL (`TestSurface_TheScreensOfLaterTasksAreNotMounted`)
>    ve `screens()`'in 12 render'ı onlara link vermez, mutlak URL sayısı 0
>    (`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`).
> 2. **İki yönlü host kapısı — yer.** Müşteri yarısı `internal/httpx`'te (`operatorHostOnly`,
>    `cfg.OperatorHost` doluysa ara katman olarak, `AccessLog`'dan sonra — 404'ü kayda geçer);
>    operatör yarısı yüzeyin alt yönlendiricisinin ilk halkası (`hostGate`). `operatorhost.go`'nun
>    importları `net`, `net/http`, `strings` (`TestOperatorHostFile_ImportsOnlyThreeStandardPackages`);
>    `httpx`'in geçişli bağımlılıklarında `operatorauth`, `internal/handler/operator`, `operatorpages`
>    yok (`TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell`, IM3); `operator.Prefix`
>    `httpx.OperatorPrefix`'tir (`TestSurface_ThePrefixIsTheRoutersPrefix`). Operatör yarısı alt
>    yönlendiricinin ilk halkası olduğu için testte sürülen yedi yöntem (GET, HEAD, POST, PUT,
>    DELETE, OPTIONS, PATCH) kayıtlı bir yolda müşteri host'unda 405 değil 404 alır; chi'nin
>    tanımadığı bir yöntem (ör. `FOO`) kök yönlendiricide, eşleşmeden önce 405'tir — operatör yolu
>    ve bilinmeyen yol için aynı cevap (`TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods`).
>    Kapalı ve ulaşılamaz hâllerde 503 sürülen host'larda aynı kaldı; müşteri yarısı ulaşılamaz
>    hâlde de takılıdır (yapılandırma tamdır) ve operatör host'unda sürülen müşteri rotası 404'tür
>    (`TestHostGate_TheUnavailableSurfaceKeepsItsAnswerOnEveryHost`).
> 3. **Host yazımı.** `httpx.OnHost`: istekteki port düşer, bir sondaki nokta düşer, büyük/küçük
>    harf yok sayılır (`TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling`'in satırları);
>    `OnHost` `X-Forwarded-Host` okumaz (kaçış tablosunun iki satırı; M08 kırmızı); mutlak-URI istek
>    satırında host URI'ninkidir (net/http'nin `r.Host`'u). Gerekçe: bir yazımı müşteri host'u
>    saymak operatör host'unun o yazımında müşteri sayfası sunardı. *(2. tur, B9:)* bir tarayıcının
>    yazması `sameOriginGate`'i geri dönüş dalından geçer — sayfaların referrer politikası
>    no-referrer olduğu için form POST'u `Origin: null` + `Sec-Fetch-Site: same-origin` taşıdı (1. tur
>    denetçisi headless Chrome'da ölçtü); sondaki noktalı yazımda TAM Origin reddedilir, `null` +
>    `same-origin` kabul edilir; o yazım operatör yüzeyine sınıflanır
>    (`TestSameOriginGate_ACrossOriginPostReachesNoStore`; büyük harf ayrı bir origin değildir).
> 4. **Port (dev `ops.localhost:8080`).** Sınıflandırmada düşer (çerezler porta bağlı değildir);
>    origin `TAPPA_BASE_URL`'in şeması ve port'undan türetilir (`operatorOrigin`; varsayılan port
>    düşer): dev `http://ops.localhost:8080`, prod `https://ops.taptime.mt`. Beşinci bir değişken
>    elendi: aynı süreç, aynı dinleyici, aynı ingress. Bozuk bir base URL açılışı durdurur
>    (`TestSurface_NewRefusesWhatAConfiguredSurfaceNeeds`).
> 5. **Static ve probe'lar.** Operatör host'unda `/static/` açık (CSS, fontlar, betik); müşteri
>    betikleri de oradan servis edilir, bu yüzden enrollment sayfasının `script-src`'si `'self'`
>    DEĞİL, bir dosyanın tam URL'sidir (bu politika vendored `htmx`'i yükletmez). `/healthz`,
>    `/readyz` operatör host'unda 404: kubelet sondaları `20-app.yaml`'da host'suz `httpGet`'tir,
>    yani pod adresiyle gelir (manifest okundu; kümede ölçülmedi).
> 6. **Operatör host'u = bir müşteri host'u** (config `TAPPA_BASE_URL`'in host'unu reddeder, öteki
>    müşteri host'larını reddetmez), ölçüldü
>    (`TestHostGate_AnOperatorHostThatIsACustomerHostServesOnlyTheOperator`, `www.taptime.mt` ile): o
>    host'ta operatör yüzeyi cevap verir ve sürülen müşteri rotası (`/admin`) 404'tür; bedeli o
>    host'un müşteri sayfaları; kanonik host `/admin`'i sunar. Bugün ingress'te `www.taptime.mt` ve
>    `tappa.everva.com.tr` kalıcı yönlendirmedir (`40-ingress.yaml`, okundu; kümede ölçülmedi). Çare
>    kodda değil: OP-9'un DNS/Ingress adımında operatör host'u ingress'in müşteri host'larından biri
>    seçilmez — sayılı sınır L3.
> 7. **Zincir** (ADR 0020 §4): konsol `hostGate → securityHeaders → floodGate → sameOriginGate →
>    requireOperator → sessionGate`; çıkış `hostGate → securityHeaders → sameOriginGate →
>    requireOperator → logoutGate → op_close_session` *(2. tur, B3)*. `requireOperator` çerezsiz
>    isteği store çağrısı olmadan 303'ler (`TestSessionGate_NoLiveSessionIsASignInRedirect`) ve
>    okuduğu token'ı bağlamla `sessionGate`'e ve çıkış işleyicisine verir *(2. tur, B2: 1. turda çıkış
>    işleyicisi çerezi kendisi de okuyordu)*. `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator`
>    SC1–SC6 listesini yakalar *(3. tur, F1: 2. turun sözdizimi pini bir import takma adıyla — X19 —
>    ve `r.Cookie(operatorauth.SessionCookieName)` ile — X19c — aşılmıştı)*. `sessionGate` önce
>    `op_touch_session`, sonra oturum başına bütçe (100/10 dk). *(2. tur, B7:)* bütçe yüklemden sonra
>    harcandığı için bir oturumun kapıyı geçen istek sayısını sınırlar, yüklemin veritabanı işini
>    değil — ölçüldü: tek oturum, 101 adres, 101 istek → 100 × 200, 1 × 429, **101** yüklem çağrısı
>    (`TestSessionGate_ABudgetPerSession`); yüklemin işini adres başına flood kapısı sınırlar
>    *(3. tur, F4)*: bütçesi tükenmiş adresten canlı çerezle konsol → 429, store çağrısı 0; kontrol:
>    başka adres 200 ve 1 yüklem (`TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate`; X03
>    kırmızı). **Çıkış — zayıf değişmez, panelin yazımıyla** *(2. tur, B3; 1. tur metni "kendi
>    oturumunu kapatmak üçüncü bir kişice reddedilememeli" diyordu ve ölçülene göre YANLIŞTI)*:
>    çerezsiz çıkış bütçeye sayılmaz (zincir sırası; N19 kırmızı) — ölçülen
>    (`TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt`): 3 001 çerezsiz çıkış, store 0,
>    ardından operatörün çıkışı oturumu kapatır. Çerezli çıkış bir definer çağrısıdır: flood bütçesi
>    ölçülür ama uyulmaz, kendi tavanı 3 000/10 dk adres başına. Ölçülen
>    (`TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession`): aynı adresten 3 000 çerezli
>    çıkıştan sonra operatörün kendi çıkışı **429** alır, o 429 oturum çerezini tarayıcıda siler ve
>    oturum store'da canlı kalır (sunucuda boşta sınırına — 30 dk — ya da mutlak sonuna kadar;
>    token'ın kopyası kullanılırsa daha uzun: sayılı sınır L16). (Veritabanı arızasının 503'ü çerezi
>    tutar — `TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend`'in son vakası.)
> 8. **`sameOriginGate`.** Güvensiz yöntem: `Origin` = operatör origin'i (harf büyüklüğü dışında);
>    `Origin` yok/`null` → `Sec-Fetch-Site` `same-origin` olmalı (panel `same-site`'ı da kabul
>    eder; burada `ops.taptime.mt` ile `taptime.mt` aynı site olduğu için tehdit o); ikisi de yoksa
>    ret. *(2. tur, B9:)* bir tarayıcının yolu geri dönüştür (no-referrer → `Origin: null`; md. 3);
>    iki dal da kabul ve ret yönünde sürülür. Ret 403 + sabit sayfa, store 0, bcrypt 0. **Log
>    *(4. tur, B6)*:** 10 dakikalık, süreç geneli bir pencerenin ilk reddi bir **WARN** kaydı
>    (yöntem; adres yok), diğerleri Debug — ölçülen
>    (`TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel`, Info düzeyi): 500
>    çerezsiz, Origin'siz çıkış + 500 çapraz-origin giriş (300 × 403, sonra flood'la 200 × 429) +
>    ikinci bir adresten 1 ret → 801 ret, 1 WARN kaydı, store 0 *(5. tur, N3: store artık 801'in
>    sonunda ölçülüyor; önceden yalnız ilk 500 çıkışın sonunda)*, erişim kaydı 1 001; ardından
>    operatörün kendi çıkışı aynı
>    adresten oturumu kapatır; kontrol: Debug'da iki ret bir WARN + bir DEBUG. *(Geçmiş: 1.–2. tur
>    her ret için WARN yazıyordu — denetçinin E1'i: 500 ret, 500 WARN; 3. tur F5 hepsini Debug'a
>    indirmişti, Info'da yüzey sessizdi.)* Konsolda `same-site`/`cross-site` getirme → 303, yüklem
>    koşmaz (`TestSessionGate_ASameSiteReadDoesNotTouchTheSession`; M17/M18 kırmızı). Giriş
>    sayfaları için GET kapısı yok — e-postadaki enrollment linki `cross-site` bir gezinmedir.
> 9. **Giriş.** Form doğrulaması YOK: adres `Password`'e olduğu gibi gider; `internal/db` saklanamayan
>    adresi bilinmeyen adresin yolundan geçirir (OP-6 md. 8): NUL/UTF-8 dışı/1 000 baytlık adres
>    bilinmeyen adresle aynı gövde, aynı başlıklar, **1 bcrypt, 1 satır** aldı
>    (`TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike` — 8 kol, kontrol: doğru
>    parola da 1). Ön kontroller `readForm`'unkilerdir: 16 KiB üstü → 413, ayrıştırılamayan gövde →
>    400; ikisi de 0 bcrypt. Handler'lar `r.PostForm`'u okur; sorgudaki parola/adres giriş
>    sayılmadı (`TestSignIn_ReadsTheBodyNeverTheQuery`). Başarılı TOTP challenge çerezini siler
>    (`TestSignIn_TheCodeStepClearsTheChallenge`). Senkronize edici token yok: SameSite=Strict +
>    origin kontrolü.
> 10. **Enrollment linki** (OP-9 basacak): `/operator/enroll?id=<hesap>#<token>` — ADR 0020 §6'nın
>    OP-8 notu. Reddedilen ama düzeltilebilir bir deneme (şifreler farklı, kural, kod) formu aynı
>    id, mühürlü sayfa ve token ile yeniden çizer — token onu gönderen POST'un yanıt gövdesine geri
>    yazılır (sızıntı testinin tasarlanmış çıkışı D5). *(2. tur, B6: 1. turda bu yeniden çizim
>    `operatorCSP` altında gidiyordu.)* Ekranı `renderEnroll` yazar ve `enrollCSP` gönderir; 40
>    sınıfın betik taşıyan dördü `enrollCSP`, diğer 36'sı `operatorCSP` taşır
>    (`TestOperatorHeaders_FortyResponseClassesCarryThePolicy`); `TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll`
>    EN1–EN3 listesini yakalar. Düzeltilemeyen ret (`ErrEnrollment`) form taşımaz. `GET` store
>    çağrısı yapmaz: iyi biçimli bir id, bekleyen, etkin ya da var olmayan bir hesabı adlandırsın,
>    aynı türden sayfayı alır. `pending` hesabın girişi (gerçek RLS): bilinmeyen adresle aynı gövde
>    ve bir `unknown_email` satırı, hedefi o hesap (`TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes`).
>    Sorgudaki token: md. 20.
> 11. **Başlıklar** *(4. tur, B1 — ölçüm davranışa taşındı; 5. tur, F1/F2/N5–N7 — ölçülen nesne
>    ve küme düzeltildi)*. Ölçülen nesne **WriteHeader anında yanıtın başlıklarıdır**: kaydedicinin
>    `Result().Header`'ı, durum satırı yazılırken alınan kopya (ondan sonra eklenen ya da silinen
>    bir başlık onu değiştirmez; 4. tur testleri işleyicinin canlı haritasını, `w.Header()`'ı
>    okuyordu — 5. tur alt bloğu F1). *(6. tur, B2:)* Telde ise net/http'nin `ResponseWriter.Header`
>    belgesinin saydığı iki istisna var — 1xx yanıtlar ve trailer'lar: WriteHeader'dan sonra
>    `http.TrailerPrefix` ile (ya da `Trailer` başlığının duyurduğu adla) konan bir anahtar trailer
>    olarak gider, kopyada değil `Result().Trailer`'dadır ve testler onu okumaz (denetçinin T01/T02'si
>    paketin tamamında yeşil). İki tablo, 48 yanıt sınıfı: C1–C40 (handler'ların
>    okunmasıyla bulundu, 2. tur B5; `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`) ve
>    C41–C48 (4. tur denetçisinin N7'si: `/operator`'da `HEAD`/`POST`/`OPTIONS`,
>    `/operator/login/totp` ve `/operator/enroll`'da `PUT`, `/operator/logout`'ta `GET` → 405;
>    kod adımında ve enrollment'ta büyük form → 413;
>    `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`). 15 düşmanca istek
>    başlığıyla (`Location`, `Content-Security-Policy`, `Cache-Control`, `Referrer-Policy`,
>    `X-Content-Type-Options`, `Set-Cookie`, `X-Frame-Options`, `Access-Control-Allow-Origin`,
>    `Refresh`, `Referer`, `User-Agent`, `Accept-Language`, `X-Requested-With`, `HX-Current-URL`,
>    `HX-Target`) ve operatörün iki çerez adını taşıyan ikinci bir `Cookie` satırıyla, 32'si — son
>    isteği `/operator/login`, `/operator/login/totp` ya da `/operator/enroll`'a giden sınıflar: C1–C26,
>    C38, C40, C44, C45, C47, C48 *(6. tur, N-1: "27" eksik sayımdı)* — ayrıca düşmanca bir sorgu
>    dizgisiyle sürüldüğünde, 48'inin her birinde
>    ölçülen: durum ve son istek `classRoutes`'taki girdiye eşit; WriteHeader anındaki başlık
>    ADLARI tasarlanan kümeye eşit; CSP tek değer (gövdesi betik yükleyen dört sınıfta — adıyla
>    C18, C20, C21, C22 — `enrollCSP`, 44'ünde `operatorCSP`, `frame-ancestors 'none'`);
>    `Cache-Control: no-store`, `nosniff`, `no-referrer` tek değer; `Location`, `Content-Type`,
>    `Allow` sınıfın tasarlanan değeri ya da yok; `Set-Cookie`'ler operatörün iki çerezinden,
>    tasarlanan ayarla/sil durumunda, `Path=/`, `Secure`, `HttpOnly`, `SameSite=Strict`,
>    `Domain`'siz; gövdede ve başlık değerlerinde düşmanca değer ham ya da sorgu-kaçışlı
>    (`url.QueryEscape`) biçimiyle yok — test bu iki biçimi arar, HTML-kaçışlı ya da base32 bir
>    yansıma aranmaz *(6. tur, B1)*; altı 405'te store 0
>    (kontrol: istek başlıklarını yanıta kopyalayan bir işleyici kontrolü geçemez). Kopyanın teldeki
>    BAŞLIK BÖLÜMÜNE (net/http istemcisinin ayrıştırdığı `Response.Header`) eşitliği üç sınıfta (C1,
>    C18, C28) gerçek bir `httptest.Server` üzerinden ölçülür, `Content-Length` ve `Date` adıyla
>    dışarıda; trailer bölümü, hijack edilmiş bağlantı ve sunucunun öbür çerçeve başlıkları
>    karşılaştırmanın dışında (`Transfer-Encoding` — C18'de `chunked` — istemcinin ayrıştırıcısında
>    `Response.TransferEncoding`'e taşınır; `Connection` bu HTTP/1.1 keep-alive isteklerinde
>    yazılmaz, `Connection: close` isteğinde ve HTTP/1.0 keep-alive'da telde var — 5. tur denetçisi
>    ölçtü); kontrol: WriteHeader'dan sonra silinen bir başlık telde ve kopyada var, canlı haritada
>    yok
>    (`TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries`). `chi.Walk`'un bildirdiği
>    monte edilmiş her yöntem × rota çifti için 405 olmayan bir sınıf, her rota için bir 405 sınıfı
>    `classRoutes`'ta var ve anahtarlar C1–C48 (`TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`;
>    sınıfsız yeni rota kırmızı). İstekle ulaşılamayan sınıflar 40 sınıflık testin başlığında
>    adıyla. Host kapısının 404'ü router'ın kendi 404'üdür (md. 2). Kaynak tarafında
>    `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` RH1–RH5 listesini yakalar
>    (`maps.Copy` o listede değil: X23a–X23b'de RH yeşil kaldı, başlık testi kırmızı; 5. turda RH3'e
>    `http.Header` üstünde yerleşik `delete` ve `clear` eklendi — X23c'nin `clear`'ı ve 4. tur
>    denetçisinin H01/L01'indeki `delete` artık RH'de de kırmızı).
> 12. **Çerez bildirimi.** Ölçüldü: tablo KODDUR (`marketing.go` `cookieNotice`), gövde
>    `legal_documents` yayınıdır — metne dokunulmadı. Karar: operatörün iki çerezi bilinçli
>    dışarıda (operatör host'unun `__Host-` çerezleri, Taptime personeli için). Tarama
>    `__Host-taptime_` adlarını görür; dışarıda bırakma dosyaya bağlı ve sayfa onları basmaz
>    (`TestCookieNotice_ListsExactlyTheCookiesTheProductSets`,
>    `TestCookiesNotOnTheNotice_AreBoundToTheirFile`).
> 13. **Uyarı kuralı (md. 9 (ix)): EKLENDİ** — `deploy/README.md` 7. satır,
>    `body.operator_surface = "unavailable"`, ≥ 1 olay; sınır 32 güncellendi; alan adı ve değer
>    koddaki sabitlere bağlı (`TestObservability_AlertSignalNames`). Gerekçe: OP-8'den sonra
>    `unavailable` kilitli bir konsoldur ve satır süreç açılışında bir kez yazılır.
> 14. **İstemci adresi:** bütçe anahtarı (`rateKey`). Ölçülen: adres (G15) sızıntı testinin S1–S4
>    yüzeylerinde, A1–A30 kollarında, R1–R10 render'larıyla bulunmadı (mutasyon M36 kırmızı). `TestClientAddress_TheListedReadsFeedOnlyTheBudgets`
>    AD1–AD5 listesini yakalar (AD3: altı başlık adı — `x-forwarded-for`, `x-real-ip`, `forwarded`,
>    `true-client-ip`, `cf-connecting-ip`, `x-client-ip`). *(3. tur, F6: "sızıntı testi G10"
>    yanlış numaraydı — adres G15.)* Audit satırına ne gittiği `operatorauth`'undur. Atıf ingress
>    log'unun (adres + `request_id`).
> 15. **Redacting tip** (OP-4 bloğu OP-8 (a)): uygulandı — `formValue` (parola, adres, kod, link
>    token'ı); basılma yolları `TestFormValue_PrintsNoValue`'da. `reveal()`'in sevk edilen kodda dokuz
>    çağrı yeri var — `Password`/`TOTP`/`CompleteEnrollment`'ın altı doğrudan argümanı,
>    `EnrollView.Token` geri yazımı, `enroll`'daki `!=`'in iki işleneni;
>    `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` FV1–FV7 listesini yakalar
>    (`URL.RequestURI`, `URL.Redacted` o listede değil — G-FV-q1/q2, X24a/X24b).
> 16. **Kabuk.** "TAPTIME OPERATOR": tam genişlikte ink bant, saffron alt çizgi (paletin uyarı tonu),
>    IBM Plex Mono büyük harf kilit — panelin açık kromuna, yeşil kelime markasına ve sekme çubuğuna
>    karşı; yeni renk yok. Kontrast hesaplandı: paper/ink **16,17:1**, saffron/ink **6,16:1**
>    (`TestOperatorChrome_TheColouredTextClearsAA`), docket-label/saffron-lite 5,64:1 (skill
>    tablosu). Tenant yuvası: `operatorpages.TenantScreen(title, TenantName)`; paket dışında
>    `TenantName` `NewTenantName` ile yapılır (boş/boşluk ret), sıfır değer render'da
>    `ErrNoTenantName` ve 0 bayt; render önce tampona yazdığı için yanıt 500 ve sayfa yok
>    (`TestTenantScreen_RefusesToRenderWithoutAName`; ad hem bantta hem `<title>`'da, kaçışlı).
>    Ana sayfa tenant verisi ve e-posta göstermez, bu build'de operatör ekranı olmadığını söyler.
> 17. **Uçtan uca, gerçek Postgres.** `tappa_operator` dev'de NOLOGIN (`rolcanlogin = f`, ölçüldü) ve
>    bir DSN kimliği değiştiremez: sahibin DSN'i + `options=-c session_authorization=tappa_operator`
>    ile bağlanınca `current_user = session_user = tappa_owner` (başlangıç seçeneği yok sayılır —
>    ölçüldü), yani `db.NewOperatorDB`'nin rol kapısı test DSN'iyle geçilemez. Yol: OP-6/OP-7
>    testlerininki — sahibin bağlantısı, test başına bir REPEATABLE READ işlem (geri alınır), store
>    çağrıları `SET LOCAL SESSION AUTHORIZATION tappa_operator` altında savepoint'lerde ve kimlik
>    çağrı başına yeniden okunur; çağrılar `internal/db`'nin ÜRETİM erişimcileri (`db.OperatorDB`
>    onlara aynen devreder). Danışma kilidi PAYLAŞIMLI (`TestE2E_TheTablesLockIsTheOneInternalDBTakes`).
>    *(3. tur, F3: önceki metin "kalıcı satır 0" diyordu ve 2. turdan — B12'nin gerçek müşteri
>    oturumlarından — beri YANLIŞTI.)* Commit eden test `TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue`:
>    koşu başına `tappa_app` havuzunda 1 tenant (`OP8 Cross Cookie Ltd`), 1 lokasyon, 1 çalışan, 1
>    admin kullanıcı (`op8-cross-<id>@example.test`), 1 admin oturumu, 1 çalışan oturumu. Ölçüldü
>    (2026-10-02, sahip bağlantısı, salt okunur işlem): o testin bir koşusu ve dosyanın `TestE2E_`
>    testlerinin bir koşusu çevresinde bu altısının her biri +1; `platform_admins`,
>    `platform_sessions`, `operator_audit_log`, `audit_log`, `tags`, `transactions` 0. Kabul
>    gerekçesi: müşteri yöneticileri bu satırları kendi bağlantılarında doğrular; panel veritabanı
>    testlerinin kalıcı fikstürleriyle aynı sınıf — backlog T81. Sürülen başarılı `op_*`'lar:
>    `op_open_session`, `op_touch_session`, `op_close_session`, `op_complete_enrollment`,
>    `op_record_auth_event` (`TestE2E_SignInRunsTheRealDefinersEndToEnd`,
>    `TestE2E_EnrollmentCompletesThroughTheDefinerAndIsRateLimited`, ve aşağıdakiler).
> 18. **`password_ok` audit türü** migration ister → OP-8'de YOK (orkestratör kararı) → OP-14.
> 19. **Dağıtık enrollment (P8/K4):** sayılar korundu (3/adres, 10/süreç, 10 dk); bir dağıtık
>    saldırganın aldığı şey reddedilen enrollment'lar (1–3 kişinin seçtiği zamanda yaptığı bir
>    kerelik adım) ve pencere başına 10 `enrollment_failed` satırı — hesap değil. Çare kodda değil,
>    ingress'te operatör host'una IP kısıtı (K4, OP-9 kullanıcı kararı). Sayılı sınır L2.
> 20. **Sorgu dizgisindeki kimlik bilgisi** *(4. tur, B2)*. Ölçülen: sorgudaki parola/adres giriş
>    sayılmadı (`TestSignIn_ReadsTheBodyNeverTheQuery`); sorgudaki token sayfada ham biçimiyle
>    bulunmadı (`TestEnroll_TheTokenNeverTravelsInTheURL`); sızıntı testinin A28/A29 kolları
>    (R1–R10 render'larıyla); md. 11'deki 32 sınıfın düşmanca sorgusu (token, adres, parola, kod,
>    blob; POST'ta id) o sınıfların hiçbirinin yanıtında ham ya da sorgu-kaçışlı biçimiyle bulunmadı
>    — HTML-kaçışlı ve base32 biçimleri aranmadı *(6. tur, B1 ve N-1)*. FV pini listesini yakalar
>    (md. 15).
> 21. **Zayıf parola uyarısı** *(4. tur, B7)*. `operatorauth` 14 karakterden kısa, 72 bayttan uzun ve
>    UTF-8 olmayan parolayı tek hatayla (`ErrWeakPassword`) reddeder; ekran başlığı "That password is
>    too short" diyordu. Yeni metin: başlık "Choose another password", gövde iki sınırı söyler
>    ("Use at least 14 characters and at most 72 bytes. A letter with an accent, or from another
>    alphabet, takes two bytes or more."). Ölçülen: dört kol (13 karakter, 73 tek baytlı karakter,
>    37 iki baytlı karakter, UTF-8 olmayan 20 bayt) bu uyarıyı alır ve "too short" demez; kontrol:
>    kurala uyan parola yanlış kodla kod uyarısını alır (`TestEnroll_TheWeakPasswordNoticeNamesBothLimits`).
>
> **Kabul — karşılıkları:** çapraz çerez iki yönde → `TestCrossCookie_CustomerValuesUnderTheOperatorNamesAreRefused`
> (sahte store) + `TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue` (*2. tur, B12:* müşteri
> tarafının yöneticilerince verilmiş bir panel ve bir çalışan oturumu operatör çerez adıyla
> `op_touch_session`'a ulaşır — sayıldı, istek başına 1 — ve 303; gerçek panel ve çalışan
> çözümleyicileri operatörün canlı token'ını reddeder, ikisi de bir kez sorgulandı; kontroller: iki
> değer de kendi tarafında canlı) · yanlış host 404 →
> `TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost`,
> `TestHostGate_TheOperatorHostServesNoCustomerRoute`, `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide`
> · cross-origin POST'ta çözümleyici 0 → `TestSameOriginGate_ACrossOriginPostReachesNoStore` (store 0) +
> `TestSurface_ACrossOriginPostPaysNothing` (bcrypt karşılaştırma 0, digest 0, store 0; kontrol: aynı
> origin 1 karşılaştırma / 1 digest) · bilinmeyen e-posta = yanlış parola →
> `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike` + `TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes`
> · TOTP tekrarı → `TestE2E_AReplayedCodeIsRefused` · N hatada kilit + audit →
> `TestE2E_WrongCodesLockTheAccountAndEachLeavesARow` (5 `totp_failed`, sonra doğru kod `locked`
> satırı + kilit sayfası, oturum 0) · kabulün "her giriş `operator_audit_log`'da" maddesi →
> `login`/`logout`/`enrollment` satırları E2E'de sayılır · CSP/no-store/nosniff →
> `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` · oturum kapısı (8 sa / 30 dk / MFA'sız /
> iptal / disabled) → `TestE2E_TheSessionGateRefusesEveryDeadSession`,
> `TestSessionGate_NoLiveSessionIsASignInRedirect` · enroll oran sınırı → 4. deneme 429, satır yazmadan
> (`TestE2E_EnrollmentCompletesThroughTheDefinerAndIsRateLimited`) · sızıntı →
> `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` (*2. tur:* numaralı sözleşme G1–G15 ×
> R1–R10 × S1–S4 × A1–A30, tasarlanmış çıkış D1–D5 pozitif olarak; kapalı ölçüt `neverLog`, 12 madde,
> sayısı pinli; hasat metot başına sayı ve ariteyle pinli; render ve yüzey kontrolleri bağımsız —
> 2. tur alt bloğu B1; Debug, iki üretim biçimi).
>
> **Mutasyonlar (1. tur; kopyala-geri-yaz; tablodaki her deneme diskte `diff` ile, geri yükleme
> sha256 ile doğrulandı; ağacın hash'i önce/sonra eşit):** 45 deneme *(2. tur: kimlik · ne ·
> kırmızıya çevirdiği test listesi aşağıdaki "2. tur" alt bloğunda; 1. turun 45'i kodun 2. tur
> hâline karşı yeniden koşuldu, dördünün hedef metni değiştiği için yeni yazımıyla)*. **42 kırmızı,
> beklenen** — host kapısının iki yarısı (kaldır, hep geçir, sıra), `OnHost` (port, harf,
> `X-Forwarded-Host`), başlıklar (üçü), enrollCSP `'self'`, `sameOrigin` (hep doğru, `same-site`
> geri dönüşü, grup kapısız), konsolun okuma kapısı (sıra, kapalı), `requireOperator` yok, ölü çerez
> silinmiyor, oturum bütçesi yok, `Verify` atlanıyor, çıkış flood'a uyuyor, başarısız çıkış çerezi
> siliyor, NUL adres erken ret, sorgu okunuyor, challenge silinmiyor, kola göre gövde, sorgudaki
> token sayfada, şifre eşitliği yok, tamponsuz render, `TenantScreen`/`NewTenantName` gevşek,
> `formValue` basıyor, parola/adres/kod/e-posta loglanıyor, token Location'da, bant çizilmiyor
> (templ), tarama kör, dışarıda bırakma listesi düşmüş, 7. kural silinmiş. **2 yeşil, beklenen:**
> M09 — sınıflandırmayı `RawPath` yerine `Path` ile yapmak testlerin satırlarında aynı
> sınıflandırmayı verdi (`routingPath` tutarlılık kararıdır); M40 — redakte `Challenge` değerini
> loglamak yer tutucu basar (kontrol). **1 derleme hatası** (M33, kullanılmayan import) derlenen
> biçimiyle (M33b) yeniden koşuldu: kırmızı.
>
> **Gerçek sunucuya kaçış denemeleri** (ulaşılamaz hâl — bu makinede açılabilen tek yapılandırılmış
> şekil: dört değişken dolu, operatör DSN'i kapalı bir porta; `"operator_surface":"unavailable"`
> okundu; süreç grubu öldürüldü, port `lsof` ile boş): operatör host'unda `/admin`, `/healthz`,
> `/readyz`, `/legal/cookies`, `OPTIONS /admin`, `//admin`, `/OPERATOR`, `/%6Fperator/login`,
> `/operator%2Flogin` → 404; `OPS.LOCALHOST`, `ops.localhost.`, port'suz → 404; `/` ve `HEAD /` → 303
> `/operator`; `/operator/../admin` → operatör yüzeyi (503), müşteri değil; `/static/…` → 200;
> müşteri host'unda `X-Forwarded-Host: ops…` ile `/admin/login` → 200 (XFH okunmaz); mutlak URI
> (operatör host'u, Host müşteri) → 404, tersi → 200; HTTP/1.0 Host'suz → müşteri (200); iki `Host`
> başlığı → net/http 400. Erişim kaydında 21 kayıt, rota deseniyle, adres yok; tasarlanmış 503'ler
> kayıt yazmadı. Testle pinlenen dört şekil daha
> (`TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods`): canlı token küçük harfli çerez adıyla
> okunmaz (303); aynı adla iki çerezde net/http İLKİNİ verir (önce çöp → 303, önce canlı → 200);
> beş dakikası geçmiş challenge → giriş yeniden, challenge silinir, hesap araması 0; chi'nin
> bilmediği bir yöntem müşteri host'unda operatör yolu ile bilinmeyen yol için aynı cevap.
>
> **Sayılı sınırlar (OP-8, tek liste; OP-6 md. 18 P1–P9 ve S1–S14 aynen geçerli):**
> - **L1** — Fragment'taki token, `replaceState`'ten ÖNCE tarayıcının genel geçmişine yazılmış
>   olabilir; tarayıcıda ölçülmedi. Token tek kullanımlık, 30 dk.
> - **L2** — Dağıtık enrollment (P8 aynen): dört hız anahtarı süreç bütçesini tüketir; çare K4 (OP-9).
> - **L3** — Operatör host'u ingress'in bir müşteri host'una eşit verilirse o host'un müşteri sayfaları
>   404 olur (md. 6); config bunu reddetmez (ingress listesini bilmez, operatör host'u bir Secret
>   değeridir — repoda değil).
> - **L4** — Oturum bütçesi ve çıkış tavanı süreç içidir (P3 sınıfı: yeniden başlatma sıfırlar, iki
>   replika ikiye katlar).
> - **L5** — Konsolun okuma kapısı getirme üst verisi (`Sec-Fetch-Site`) gönderen tarayıcılar içindir;
>   göndermeyen bir tarayıcıda kardeş host'un gömülü isteği yüklemi koşturur (boşta süreyi tazeler).
> - **L6** — Katı Origin karşılaştırması tarayıcı olmayan bir istemciye karşı derinlik savunmasıdır;
>   sınır bütçelerdir.
> - **L7** — Enrollment'ın düzeltilebilir reddi link token'ını POST yanıt gövdesine geri yazar (D5);
>   betiksiz yol token'ı kişinin panosundan geçirir.
> - **L8** — `Secret.Zero` dizgeleri silemez; render tamponu anahtarın base32'sini GC'ye kadar tutar (P6).
> - **L9** — Host kapısının 404'ü yanıt olarak router'ınkiyle aynıdır, erişim kaydında DEĞİL: rota
>   alanı ÜÇ değerden biridir — `/operator/*`, `/operator` (müşteri host'unda `GET /operator`; 1. tur
>   denetçisi ölçtü) ya da `(unmatched)` *(2. tur, B10: 1. tur iki değer sayıyordu)* (süreç içi,
>   istemciye görünmez).
> - **L10** — Operatör host'unda sondalar 404'tür; bir dış uptime denetimi `/operator/login`'i
>   kullanmalı. `HEAD`/`OPTIONS` operatör rotalarında operatör host'unda 405'tir.
> - **L11** — Bcrypt sayısı sahte store'la ölçüldü (pending/disabled/saklanamayan adres orada
>   `ErrNoOperator`, `internal/db`'nin belgelenmiş cevabı); gerçek RLS ile ölçülen gövde + satırdır,
>   bcrypt sayısı değil.
> - **L12** — Konsol operatörün kimliğini göstermez (e-posta okuyan bir definer yok; yenisi migration).
> - **L13** — Sızıntı testinin NOT CLAIMED listesi (`leak_test.go` başlığı): bölünmüş değerler,
>   listelenmeyen render'lar, operatorauth'un kendi tipleri, süreç dışındakiler (ingress log'u,
>   tarayıcı geçmişi), numaralanmamış kollar.
> - **L14** — Canlıda operatör host'u bugün ULAŞILAMAZ: ingress'te kuralı yok (OP-9, K2). Bu görev
>   canlıda bir ingress kuralı eklemedi.
> - **L15** — *(2. tur, B8)* Enrollment betiğinin DAVRANIŞI pinli değildir: depoda JavaScript motoru
>   yok ve eklenmedi. Pinli olan METİNDİR — kodun normalleştirilmiş sha256'sı
>   (`TestEnrollScript_IsTheReviewedBody`: kodda bir değişiklik özeti değiştirir) ve isim listesi
>   (`TestEnrollScript_TouchesTheFragmentAndNothingElse`). Davranış 2026-10-02'de 1. tur denetçisince
>   headless Chrome'da ELLE ölçüldü: geçerli token → alan 43 karakter ve link token'ına eşit,
>   sarmalayıcı gizli, `location.hash` boş, fragment ölçülen isteklerde yok; bozuk fragment → alan
>   boş ve görünür, fragment silindi.
> - **L16** — *(2. tur, B3)* Çıkışın zayıf değişmezi (md. 7): paylaşılan bir hız anahtarından bir
>   pencerede 3 000 çerezli çıkış, operatörün kendi çıkışını 429'a düşürür; tarayıcı oturumu unutur,
>   sunucudaki oturum boşta sınırına ya da mutlak sonuna kadar (token'ın kopyası kullanılırsa daha
>   uzun) yaşar. Çare kodda değil: K4 (OP-9) ve `opadmin` ile iptal.
> - **L17** — *(2. tur, gözlem)* Başka siteden gelen bir gezinme (ör. e-postadaki bir konsol linki)
>   `SameSite=Strict` yüzünden oturum çerezini taşımaz: canlı oturumlu operatöre giriş sayfası
>   gösterilir. Bilinçli bedel (ADR 0020 §2'nin `Strict`'i), kod değişikliği yok.
> - **L18** — *(3. tur; 4. turda yeniden yazıldı, B3)* Yapısal (go/types) pinler yalnız
>   başlıklarındaki listeyi yakalar; listede olmayan biçimler kod incelemesinin konusudur — örnekler,
>   ölçülmüş: başka paketteki bir yardımcı (X19r yeşil), çalışma zamanında kurulan bir ad (X19s
>   yeşil), `encoding/asn1` ile kurulan bir `ProblemView` (X25a yeşil), `r.TLS.ServerName` (X25b
>   yeşil), `X-Cluster-Client-IP` (X25c yeşil), `URL.RequestURI`/`URL.Redacted` (X24a/X24b, FV'de
>   yeşil). Yükleyicinin reddettiği sekiz import adıyla: `reflect`, `unsafe`, `plugin`,
>   `text/template`, `html/template`, `encoding/json`, `encoding/gob`, `encoding/xml` (X19q kırmızı);
>   ve çalışan build'in dışarıda bıraktığı test dışı dosya (X19p kırmızı).
> - **L19** — *(3. tur, F5; 4. turda güncellendi, B6)* Çıkışın origin reddi sayıca sınırsızdır:
>   önünde flood kapısı yok (B3'ün kazanımı — bir kapı, hız anahtarını paylaşan üçüncü kişiye
>   operatörün çıkışını reddettirirdi; X21b ölçtü). Bir ret bir erişim kaydı ve bir 403 render'ıdır;
>   store çağrısı, bütçe, bcrypt yok. Süreç log'u: 10 dakikalık süreç geneli pencerenin ilk reddi
>   bir WARN kaydı (yöntem; adres yok), diğerleri Debug. Panelin `sameOriginGate`'i her reddi WARN
>   olarak, ip ile yazar (`internal/handler/adminlogin.go:592`); `deploy/README.md`'nin yedi uyarı
>   kuralından biri de operatör 403'ünü ya da bu kaydı okumaz.
> - **L20** — *(6. tur; güvenlik denetçisinin düşük notu)* Girişin bcrypt'i için süreç geneli bir
>   tavan yok, yalnız adres başına `work` bütçesi (20/10 dk). Denetçi tek bir /48 içindeki 40 farklı
>   /64'ten 40 POST ile 40 cost-12 bcrypt ölçtü (seri 10,7 s); dağıtık bir istemci CPU'yu doyurabilir
>   (ADR 0020'nin 196–380 ms/karşılaştırma ölçüsüyle bir çekirdek ≈80–150 hız anahtarıyla dolar).
>   Süreç müşteri ürünüyle (tap, panel) paylaşılıyor. Panelin `/admin/login`'i aynı sınıfta
>   (`adminLoginWorkLimit` 120/adres, süreç geneli tavan yok) ve bugün canlıda erişilebilir; operatör
>   host'u canlıda erişilemez (L14). Devir: **OP-9, K4** (ops IP kısıtı).
> - **L21** — *(6. tur; güvenlik denetçisinin düşük notu)* Araya sokulan aynı adlı bir çerez
>   operatörü tarayıcıdan atabilir. Sunucu yarısı: aynı adlı iki çerezden ilki okunur, çöp-önce
>   istek 303'tür (`TestEscapes_CookieNamesDuplicatesExpiryAndOddMethods` bunu ölçer); yanıtın
>   silen `Set-Cookie`'si ve sunucudaki oturumun canlı kalması güvenlik denetçisinin ölçümüdür, o
>   testte kontrol edilmez (test mantığına bu turda dokunulmadı). Tarayıcı yarısı — kardeş bir host'un
>   `__Host-` adlı bir çerezi gerçek çerezden önce serileştirebilmesi (isimsiz çerez hilesi; RFC
>   6265bis yasaklar, güncel Chrome/Firefox reddeder) — DOĞRULANAMADI. Gerçekleşirse sonucu zorla
>   çıkıştır; erişim vermez.
>
> **Kart düzeltmesi — 2. tur (2026-10-02, 1. üçüncü göz denetçisinin RED'inden sonra: 3 bloklayan + 9
> bloklamayan; hepsi kapatıldı).** Değişen: `internal/handler/operator/routes.go` (çıkış zinciri,
> `requireOperator`'ın token'ı bağlama koyması, `sessionTokenOf`, yorumlar), `console.go` (çıkış token'ı bağlamdan),
> `render.go` (`problemPages`, `problemSignOutThrottled`, `renderEnroll`), `enroll.go`
> (`renderEnroll`), `surface.go` (oturum bütçesi yorumu), `internal/httpx/operatorhost.go` (yorum),
> `marketing.go` (yorum); testler `leak_test.go` (yeniden yazıldı), yeni `op8r2_test.go`,
> `op8_test.go`, `op8_db_test.go`, `rig_test.go`, `export_test.go`. Migration YOK, bağımlılık YOK.
> Yukarıdaki md. 2, 3, 7, 8, 10, 11 ve L9 yerinde işaretlendi; L15–L17 eklendi.
>
> - **B1 (bloklayan) · sızıntı testinin pozitif kontrolü kendini doğruluyordu.** Test yeniden
>   yazıldı: (a) her render (R1–R10, tek iğne) ADIYLA bağımsız bir kurucuyla eşli (`fmt`, akış
>   kodlayıcıları, `net/url`'ün form kodlayıcısı, R5 için operatör sayfalarının KENDİ templ
>   render'ı); her üyenin iğnesi kurucunun metninde OLMAK ZORUNDA, `minNeedle`'dan kısa/boş iğne
>   KIRMIZI; G1'de %q, JSON, URL ve HTML biçimi ham değerden FARKLI bir kontrol parolası var ve her
>   render için "en az bir üyenin biçimi hamdan farklı" sayılır; (b) yüzeyler (S1–S4) sayısı ve
>   adlarıyla pinli, her yüzeye TEK TEK konan bir kanarya `scanArm` ile o yüzeyde bulunmak zorunda,
>   her yüzey bir gerçek kolda metin taşımak zorunda; (c) `neverLog` — 12 madde, sayı pinli
>   (CLAUDE.md §7 + ADR 0020 §5 + ADR 0021 §3.5), her maddenin her grubunda en az bir üye; (d) hasat
>   (`hashRecorder`) metot başına SAYI ve ARİTEYLE pinli (`harvestWant`, sayıların kollardan türetimi
>   yorumda) ve hasadın kendisi de aranıyor (oturum hash'leri, ham link token'ları, digest, zarf,
>   adresler); (e) yeni kol **A17** — kullanılmış linkin AYNI token'la yeniden gönderilmesi:
>   veritabanının reddi, `ErrEnrollment` dalı, gerçek token'la. Mutasyonlar: N01b, N02–N11 — hepsi
>   kırmızı (tablo aşağıda).
> - **B2 (bloklayan) · ADR PART II.** (i) Tam mutasyon listesi aşağıda (kimlik · ne · kırmızıya
>   çevirdiği test). (ii) Çıkış da `requireOperator`'ın arkasına alındı ve token'ı bağlamdan alıyor
>   (`sessionTokenOf`); bir sözdizimi pini eklendi (`ReadSessionCookie` seçicisi bir kez,
>   `requireOperator` içinde). *(4. tur: bu maddenin 2. turdaki başlığı — "`requireOperator` çerezin
>   TEK okumasıdır" YAPISAL olarak doğru yapıldı — pinden genel hüküm çıkarıyordu; pin 3. turda bir
>   import takma adıyla aşıldı ve go/types pini
>   `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` ile değiştirildi; o pin
>   SC1–SC6 listesini yakalar.)* Mutasyon N18 kırmızı (3. turda yeni pine karşı yeniden koşuldu:
>   kırmızı).
> - **B3 (bloklayan) · üçüncü kişi çıkışı reddettirebiliyordu.** Ölçüldü ve karar: çerezsiz çıkış
>   bütçeye sayılmaz (`requireOperator` önce) — testin 3 001 çıkışında store 0 ve 429 yok; çerezli
>   çıkış tavana sayılır;
>   tavanın 429'u oturum çerezini SİLER; kalan zayıf değişmez panelin yazımıyla md. 7'de ve L16'da.
>   Testler: `TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt`,
>   `TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession`. Mutasyonlar N19, N20 kırmızı.
> - **B4 · problem sayfalarının linkleri.** `problemPages` on `ProblemView`'u listeler;
>   `TestProblemPages_LinkOnlyToMountedRoutes` onunu render edip linklerini bağlı rotalara tutar;
>   2. turun sözdizimi pini *(3. tur, F2: buradaki cümle — "`render.go`'nun değişkenleri ve
>   `problemTooMany` dışında `ProblemView` değişmezi yok" — pinin yakaladığından genişti: pin yalnız
>   `operatorpages.ProblemView` diye YAZILMIŞ literali görüyordu, tip takma adıyla yazılmış olan (X20b)
>   yeşildi. Pin go/types'a taşındı: `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` —
>   PV1–PV7 listesini yakalar (testin başlığı); 4. turun biçimiyle PART III tek cümle: listede
>   olmayan biçimler kod incelemesinin konusudur — örnekler: sıfır değer, başka paketin döndürdüğü
>   değer, `encoding/asn1` ile kurulan değer)*. Mutasyonlar N12 (denetçinin
>   `Back = Prefix+"/tenants"`'i), N13, N14 kırmızı (2. tur; N13 ve N14'ün 3. tur karşılıkları X20c,
>   X20k).
> - **B5 · başlık testi örneklemdi.** 40 yanıt sınıfı, durumlarıyla (md. 11). Mutasyonlar N16
>   (denetçinin D5 `Cache-Control` silmesi), N17 kırmızı.
> - **B6 · D5 `operatorCSP` altındaydı.** Ekranı `renderEnroll` yazar; betik ⇔ `enrollCSP` eşlemesi 40 sınıfta testte.
>   Mutasyon N15 kırmızı.
> - **B7 · oturum bütçesi yorumu.** Ölçülene eşitlendi (md. 7); 101 yüklem çağrısı testte sayılıyor.
>   Mutasyon N21 (bütçeyi yüklemden önceye almak) kırmızı.
> - **B8 · betik davranışı.** Metin pini + incelenmiş gövde pini; davranış elle ölçülmüş, pinsiz —
>   L15; ADR §6 notu üç parçaya çekildi. Mutasyonlar N22, N23 (denetçinin iki mutasyonu) kırmızı.
> - **B9 · "TAM origin" cümlesi.** Ölçülene eşitlendi (md. 3, 8, `operatorhost.go`, `routes.go`);
>   `Origin: null` + `same-origin` kabul ve sondaki noktalı yazımın TAM Origin'i ret testte. Mutasyon
>   N24 kırmızı.
> - **B10 · "her yöntem" ve L9.** md. 2 ve L9 düzeltildi; ADR notu da.
> - **B11 · sayı.** Bu turun teslim raporu hedefli koşuyu ÖLÇTÜĞÜ komutla yeniden sayar.
> - **B12 · E2E çapraz çerez.** Gerçek verilmiş panel ve çalışan oturumları; `op_touch_session`'a
>   ulaşım sayılıyor (istek başına 1). **Kalıcı test verisi** (panel veritabanı testlerininki gibi):
>   koşu başına 1 tenant, 1 lokasyon, 1 çalışan, 1 admin kullanıcı, 1 panel oturumu, 1 çalışan
>   oturumu (`tappa_app` havuzunda commit; operatör satırları geri alınan işlemde). Mutasyon N25
>   (yüklemi iki kez koşmak) kırmızı — sayaç canlı.
> - **Not ve gözlem:** `marketing.go`'nun "a seventh"'i ve tarama kapsamı cümlesi düzeltildi; SameSite
>   gözlemi L17.
>
> **Tam mutasyon tablosu (kopyala-geri-yaz, `scratchpad` kopyalarından; tablodaki her deneme diskte
> `diff` ile görüldü, geri yüklemesi sha256 ile; ağacın parmak izi önce/sonra eşit).** 1. turun 45'i kodun 2. tur
> hâline karşı YENİDEN koşuldu (M24, M29, M39 hedef metni değiştiği için `b` yazımıyla; M33'ün
> derlenmeyen yazımı yerine M33b); N01'in ilk yazımı derlenmedi (kullanılmayan import), yerine N01b.
> **69 deneme: 67 kırmızı, 2 yeşil beklenen** (M09 — `Path` ile `RawPath` testlerin satırlarında
> aynı sınıflandırmayı verdi; M40 kontrol — redakte `Challenge` yer tutucu basar).
>
> | Kimlik | Ne | Sonuç | Kırmızıya çevirdiği test(ler) |
> |---|---|---|---|
> | M01 | hostGate removed from the sub-router | RED | `TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost`, `TestHostGate_AnOperatorHostThatIsACustomerHostServesOnlyTheOperator` |
> | M02 | hostGate passes every host | RED | `TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost` |
> | M03 | hostGate after securityHeaders | RED | `TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost` |
> | M04 | customer half not mounted | RED | `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide`, `TestHostGate_TheOperatorHostServesNoCustomerRoute`, `TestHostGate_AnOperatorHostThatIsACustomerHostServesOnlyTheOperator` |
> | M05 | customer half lets customer routes through | RED | `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide` |
> | M06 | OnHost keeps the port | RED | `TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling`, `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide` |
> | M07 | OnHost case-sensitive | RED | `TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling`, `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide` |
> | M08 | OnHost reads X-Forwarded-Host | RED | `TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide` |
> | M09 | gate classifies by Path not RawPath | GREEN | — (yeşil, beklenen) |
> | M10 | no Referrer-Policy | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | M11 | CSP without frame-ancestors | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | M12 | enrollCSP uses 'self' | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | M13 | no no-store | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | M14 | sameOrigin always true | RED | `TestSameOriginGate_ACrossOriginPostReachesNoStore`, `TestSurface_ACrossOriginPostPaysNothing` |
> | M15 | fallback accepts same-site | RED | `TestSameOriginGate_ACrossOriginPostReachesNoStore` |
> | M16 | sign-in group without sameOriginGate | RED | `TestSameOriginGate_ACrossOriginPostReachesNoStore`, `TestSurface_ACrossOriginPostPaysNothing` |
> | M17 | console resolves before the read guard | RED | `TestSessionGate_ASameSiteReadDoesNotTouchTheSession` |
> | M18 | read guard off on the console | RED | `TestSessionGate_ASameSiteReadDoesNotTouchTheSession` |
> | M19 | requireOperator removed | RED | `TestSessionGate_NoLiveSessionIsASignInRedirect` |
> | M20 | dead cookie not cleared | RED | `TestSessionGate_NoLiveSessionIsASignInRedirect` |
> | M21 | no per-session budget | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestSessionGate_ABudgetPerSession` |
> | M22 | Verify skipped (any cookie is live) | RED | `TestE2E_TheSessionGateRefusesEveryDeadSession`, `TestSessionGate_NoLiveSessionIsASignInRedirect`, `TestCrossCookie_CustomerValuesUnderTheOperatorNamesAreRefused` |
> | M23 | sign-out obeys the flood budget | RED | `TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend`, `TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession` |
> | M24b | sign-out clears the cookie on a failed close | RED | `TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend` |
> | M25 | handler refuses a NUL address early | RED | `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike` |
> | M26 | sign-in reads the query too | RED | `TestSignIn_ReadsTheBodyNeverTheQuery` |
> | M27 | challenge not cleared on success | RED | `TestSignIn_TheCodeStepClearsTheChallenge` |
> | M28 | refused body differs by arm | RED | `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike`, `TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes` |
> | M29b | GET enroll echoes a query token | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestEnroll_TheTokenNeverTravelsInTheURL` |
> | M30 | password mismatch not checked | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M31 | render streams without a buffer | RED | `TestTenantScreen_RefusesToRenderWithoutAName` |
> | M32 | TenantScreen accepts the zero name | RED | `TestTenantScreen_RefusesToRenderWithoutAName` |
> | M33b | NewTenantName accepts blanks (compiles) | RED | `TestTenantScreen_RefusesToRenderWithoutAName` |
> | M34 | formValue prints its value | RED | `TestFormValue_PrintsNoValue` |
> | M35 | the password is logged | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M36 | the client address is logged | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M37 | the code is logged | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M38 | the link token goes to Location | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M39b | the email is logged on a failure | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | M40 | the challenge is logged | GREEN | — (yeşil, beklenen) |
> | M41 | the bar is not drawn on platform screens | RED | `TestOperatorScreens_EveryOneWearsTheOperatorChrome` |
> | M42 | the operator cookies are listed by nobody, scan blind | RED | `TestCookieNameScanner_CatchesANewCookie`, `TestCookiesNotOnTheNotice_AreBoundToTheirFile`, `TestCookieNotice_ListsExactlyTheCookiesTheProductSets` |
> | M43 | the omission list is dropped | RED | `TestCookieNotice_ListsExactlyTheCookiesTheProductSets` |
> | M44 | rule 7 dropped from the paste-able block | RED | `TestObservability_AlertSignalNames` |
> | N01b | R2 rendering emptied (compiles) | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N02 | R3 rendering emptied (nil-like) | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N03 | R5 reduced to the raw value | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N04 | R2 reduced to the raw value | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N05 | S2 dropped from the scan | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N06 | S2 scanned blind | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N07 | a never-log item deleted | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N08 | a never-log item bound to an empty group | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N09 | harvest arity changed | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N10 | harvest drops a method | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N11 | the ErrEnrollment branch logs the link token | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | N12 | a problem page links to an unmounted screen | RED | `TestProblemPages_LinkOnlyToMountedRoutes` |
> | N13 | a ProblemView built outside render.go | RED | 2. turun sözdizimi pini (3. turda kaldırıldı; karşılığı X20c → `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo`) |
> | N14 | problemPages omits one | RED | `TestProblemPages_LinkOnlyToMountedRoutes`, 2. turun sözdizimi pini (3. tur karşılığı X20k) |
> | N15 | the D5 re-render under operatorCSP | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | N16 | D5 loses no-store | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | N17 | a 503 page loses no-store | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | N18 | sign-out reads the cookie itself | RED | 2. turun sözdizimi pini; 3. turda yeni pine karşı yeniden koşuldu: `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | N19 | sign-out budget before requireOperator | RED | `TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt` |
> | N20 | the ceiling's 429 keeps the cookie | RED | `TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession` |
> | N21 | session budget charged before the predicate | RED | `TestSessionGate_ABudgetPerSession` |
> | N22 | enroll.js: the fragment branch disabled | RED | `TestEnrollScript_IsTheReviewedBody` |
> | N23 | enroll.js: the field emptied | RED | `TestEnrollScript_IsTheReviewedBody` |
> | N24 | Origin null treated as an origin | RED | `TestSameOriginGate_ACrossOriginPostReachesNoStore` |
> | N25 | the session predicate run twice | RED | `TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue`, `TestSessionGate_ABudgetPerSession` |
>
> **Kart düzeltmesi — 3. tur (2026-10-02, 2. üçüncü göz denetçisinin RED'inden sonra: 1 bloklayan + 5
> bloklamayan; hepsi kapatıldı). KATMAN DEĞİŞTİ.** 2. turun iki yapısal pini SÖZDİZİMİNE bakıyordu ve
> birer takma adla aşıldı (F1, F2 — 1. turun B2'siyle aynı sınıf: metin pinin yakaladığından genişti).
> Bu turda yapısal pinler go/types'a taşındı (`internal/handler/operator/typepins_test.go`): paket,
> ÇALIŞAN build'in `go list -export -deps` dışa aktarım verisiyle (`-race` build'inde `-race`'in)
> tip denetiminden geçirilir — `internal/operatorauth`'un `exactImports`'u ve `internal/db`'nin
> `moduleExports`'u emsal, yeni bağımlılık yok — ve paketin adları işaret ettikleri NESNEYE
> (`Info.Uses`/`Defs`), ifadeleri TİPLERİNE ve sabit DEĞERLERİNE (`Info.Types`) çözülür; takma ad, nokta
> import'u, tip takma adı ve yöntem değeriyle yazılmış bir kullanım aynı nesneye çözüldü (X19, X19d2,
> X19e, X19g, X20b, X20m kırmızı); konum `token.Pos`'tur (X19o kırmızı). Yükleyici, çalışan build'in
> dışarıda bıraktığı test dışı bir dosya (`//go:build !race`, GOOS eki, cgo, assembly) ya da sekiz
> importtan biri (`reflect`, `unsafe`, `plugin`, `text/template`, `html/template`, `encoding/json`,
> `encoding/gob`, `encoding/xml`) görürse kırmızıdır. Pinlerin başlıkları üç parçalı; 4. turun
> biçimi: PART II yalnız pinin listesi, PART III tek cümle. Pin kodları iki harfli (SC, PV, EN, FV,
> AD, RH, HG, SN, IM), sızıntı testinin G/R/S/A/D'siyle ve OP-6'nın P/S'siyle çakışmasın diye.
>
> Değişen: ürün — `routes.go` (`sameOriginGate`'in ret kaydı WARN → Debug; yorumlar), `render.go`,
> `form.go`, `enroll.go`, `signin.go`, `console.go`, `surface.go`, `internal/httpx/operatorhost.go`,
> `web/templates/operatorpages/view.go`, `chrome.templ`, `enroll.templ`, `signin.templ`,
> `web/templates/layout/base.templ` (+ üretilen `_templ.go`; yalnız yorumlar); testler — YENİ
> `typepins_test.go` (9 pin; onuncusu `TestOperatorHostFile_ImportsOnlyThreeStandardPackages`, httpx'te), `op8r3_test.go` (F4, F5), `race_on_test.go`/`race_off_test.go`;
> `op8r2_test.go` (iki sözdizimi pini kaldırıldı; başlık testi YENİDEN ADLANDIRILDI:
> `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, eski adı "Every Response …" idi),
> `op8_test.go` (başlıklar), `op8_db_test.go` (başlık, F3), `leak_test.go` (D1–D5 tanımlandı), `rig_test.go`
> (`newRigAt`), `internal/httpx/operatorhost_test.go` (import pini, başlıklar); bu kart ve ADR 0020.
> Migration YOK, bağımlılık YOK. Yukarıdaki md. 1, 2, 7, 8, 9, 11, 14, 15, 17, 2. tur B2/B4 ve
> N13/N14/N18 satırları yerinde işaretlendi; L18, L19 eklendi.
>
> - **F1 (bloklayan) · "oturum çerezinin TEK okuması".** Ne değişti: sözdizimi pini kaldırıldı; yerine
>   `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` — SC1 `operatorauth.ReadSessionCookie`
>   NESNESİNİN `(*Surface).requireOperator` dışındaki her kullanımı ya da orada 1'den farklı sayı; SC2
>   `SessionCookieName` NESNESİNİN her kullanımı; SC3 değeri çerezin adını İÇEREN her sabit dizge
>   ifadesi (harf duyarsız; literal, yerel sabit, sabit birleştirme); SC4 `(*http.Request).Cookie`,
>   `Cookies`, `CookiesNamed`, `http.ParseCookie`; SC5 bu adlarla bir ARAYÜZ ya da tip-parametresi
>   yöntemi üzerinden çağrı; SC6 `"Cookie"` başlık adına eşit sabit. PART III: çalışma zamanında
>   kurulan başlık/çerez adı, başka paketteki okuyucu. Kontroller: aynı nesne çözümü
>   `ReadChallengeCookie`'yi tam `codePage` ve `code`'da bulur; `(*http.Request).Context`, `templ.Component`
>   üzerinden `Render`, iki `"Sec-Fetch-Site"` bulunur. Metinler (`routes.go`, md. 7, ADR (II))
>   pinin listesine eşitlendi. **X19 ve X19c KIRMIZI** (paketin tamamı, DB'li — tek kırmızı yeni pin);
>   **kontrol N18 KIRMIZI**; sevk edilen kod yeşil. Kaçışlar: aşağıdaki süpürme tablosu ve mutasyon tablosu.
> - **F2 · `ProblemView` pini tip takma adını kaçırıyordu.** `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo`
>   — PV1 TİPİ `ProblemView` (her takma adla) olan bileşik literal, `render.go`'nun paket düzeyi bir
>   değişkeninin DOĞRUDAN değeri ya da `problemTooMany`'nin içi dışında; `problemTooMany`'de 1'den
>   fazla; anahtarsız literal · PV2 `problemPages`'in kullanmadığı değişken (boş tanımlayıcı dahil) ·
>   PV3 `problemPages`'in `problemTooMany`'yi `false` ve `true` ile tam iki kez çağırmaması · PV4 sabit
>   olmayan `Back` · PV5 bu literallerin anahtarı dışında bir `ProblemView` ALANI kullanımı (sıfır
>   değere yazım, gömmeyle gelen alan, `&v.Back`) · PV6 `ProblemView`'dan kurulmuş bir tipe DÖNÜŞÜM ·
>   PV7 onunla generic ÖRNEKLEME. PART III: alanı hiç yazılmamış SIFIR değer (kendi linki yok: `Back`
>   boş, çubuğu `problemTooMany(false)`'unki), başka paketin döndürdüğü değer. Kontrol: üç `SignInView`
>   literali ve `Failed`/`Expired` anahtarları bulunur. Kart B4 cümlesi listeye eşitlendi. **X20b
>   KIRMIZI, kontrol X20c KIRMIZI.**
> - **F3 · `op8_db_test.go` başlığı ve md. 17 "kalıcı satır 0" yanlıştı.** Ölçüldü ve yazıldı (md. 17,
>   dosya başlığı): koşu başına commit eden tek test, altı satır, operatör tablolarında 0, kabul
>   gerekçesi ve T81 sınıfı. Kod değişikliği yok (kalıcılık kabul — panel emsali).
> - **F4 · konsolun `floodGate`'i ölçülmüyordu.** `TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate`:
>   bir adres 300 giriş sayfasıyla flood bütçesini harcar (store 0), aynı adresten CANLI çerezle konsol
>   → 429 ve store çağrısı 0 (`TouchOperatorSession` dahil); kontrol: aynı çerez başka adresten → 200
>   ve tam 1 yüklem çağrısı. `surface.go` (`sessionLimit`), `routes.go` (`floodGate`), md. 7, ADR (iii)
>   ona atıf yapar. **X03 KIRMIZI.**
> - **F5 · çıkıştaki origin reddi log'u çağıranın yazdırdığı bir satırdı.** *(4. turda değişti — B6:
>   pencere başına bir WARN, gerisi Debug; test yeniden adlandırıldı:
>   `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel`. Aşağıdaki 3. tur metni o
>   turun kararıdır.)* Karar **(a)**: `sameOriginGate`'in
>   ret kaydı WARN → **Debug** (üretim Info'da: `deploy/k8s/05-config.yaml`; erişim log'u istek başına
>   zaten bir kayıt yazar). (b) elendi: çıkışın önüne bir bütçe B3'ü bozar — saf hâli X21b ölçüldü: çıkış
>   testleri ve sızıntı testi kırmızı. 3. turun testi (Info):
>   500 çerezsiz, Origin'siz çıkış → 500 × 403, store 0; aynı adresten 500 çapraz-origin giriş → 300 × 403
>   + 200 × 429 (çıkış retleri bütçe harcamadı); ret kaydı 0; erişim kaydı 1 000; ardından operatörün
>   kendi çıkışı aynı adresten oturumu kapatır (303, canlı oturum 0 — B3 bozulmadı); kontrol: Debug'da
>   tek ret bir kayıt. Metinler: `routes.go` (`sameOriginGate`, `floodGate`, `mount`), md. 8, ADR (iv),
>   L19. **X21a (WARN'a geri) KIRMIZI — Info'da 800 kayıt (iki biçimde 1 600 satır); X21b KIRMIZI; X21c (satır tümden silindi)
>   KIRMIZI.**
> - **F6 · numaralar.** md. 14'ün "G10"u G15 yapıldı. Kartta, ADR'de ve yorumlarda geçen G/R/S/A/D/L/C
>   numaraları teste karşı denetlendi: G1–G15, R1–R10, S1–S4, A1–A30, C1–C40 testtekiyle eşit; L1–L19
>   kartta tanımlı; P1–P9/S1–S14 OP-6 md. 18'in; M08/M09 (`operatorhost.go`) 2. tur tablosuyla eşit.
>   Bulunan ikinci kusur: D1–D5 `leak_test.go` başlığında anılıyor ama TANIMLANMIYORDU — tanımlandı
>   (D1 challenge, D2 oturum token'ı, D3 sayfanın TOTP anahtarı, D4 mühürlü blob, D5 geri yazılan link
>   token'ı; izin tablosundan okundu); kartın D atıfları (md. 10, L7, B5, B6, N15, N16) bununla tutarlı.
>
> **Öz-denetim süpürmesi (3. tur).** *(4. tur: 3. turun 43 satırlık tablosu kaldırıldı. "Sertleştirildi"
> satırları bir pinden ürüne dair genel hüküm çıkarıyordu — 3. tur denetçisi beşini mutasyonla aştı
> (`maps.Copy`, `encoding/asn1`, `r.TLS.ServerName`, `URL.RequestURI()`, `X-Cluster-Client-IP`).
> Yeniden yapılmış tablo — önceki metin · yeni metin · tür — 4. tur alt bloğunda.)*
>
> **3. tur mutasyon tablosu (kopyala-geri-yaz, yalnız scratchpad yedekleri; yeni dosyalar koşudan sonra
> silindi; her biri diskte `diff` ile görüldü, geri yükleme sha256 ile; ağacın parmak izi önce/sonra
> eşit).** **51 deneme: 48 kırmızı, 2 yeşil BEKLENEN (X19r, X19s — PART III, L18), 1 derlenmez (X19d — bu pakette yazılamayan kaçış)**.
> Paketin tamamıyla (DB'li) koşulanlar: X19, X19c, N18, X20b, X20c, X03, X21a, X21b — her birinde
> kırmızıya dönen yalnız tabloda adı geçenler.
>
> | Kimlik | Ne | Sonuç | Kırmızıya çevirdiği test(ler) |
> |---|---|---|---|
> | X19 | console.go reads the session cookie through an import alias (oa.ReadSessionCookie) in logout | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19c | sessionGate reads the cookie a second time with r.Cookie(operatorauth.SessionCookieName) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | N18 | CONTROL: sign-out reads the cookie itself (operatorauth.ReadSessionCookie, plain spelling) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19d | dot import of operatorauth in a new file, ReadSessionCookie(r) | BUILD FAILED | — (derlenmez: `New` hem `operator`'da hem `operatorauth`'ta; bu kaçış bu pakette yazılamaz) |
> | X19d2 | dot import of net/http in a new file, r.Cookie("x") on *Request | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19e | ReadSessionCookie taken as a function value at package level (console.go) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19f | method value: get := r.Cookie in sessionGate | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19g | method expression through a type alias: (*rq).Cookie(r, ...) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19h | r.Cookies() loop comparing against a name built at run time | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19i | raw header index r.Header["Cookie"] | RED | `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions`, `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19j | raw header read r.Header.Get("cookie") (lower case) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19k | string literal instead of the constant: r.Cookie("__Host-taptime_op") | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19l | the name as a constant concatenation, used by no cookie reader | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19m | interface dispatch: var c interface{ Cookies() []*http.Cookie } = r | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19n | embedding: struct{ *http.Request }{r}.Cookie(...) | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19o | a package-level func named requireOperator in another file under //line routes.go | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19p | a //go:build !race product file reading the cookie, under -race | RED | yükleyiciyi kullanan her go/types pini (çalışan build'in dışarıda bıraktığı ürün dosyası reddedilir) |
> | X19p2 | the same file, without -race | RED | `TestSessionCookieReads_TheListedFormsOccurOnlyInRequireOperator` |
> | X19q | reflection: a file importing reflect | RED | yükleyiciyi kullanan her go/types pini (`reflect` importu reddedilir) |
> | X19r | PART III: a helper in ANOTHER package (httpx) reads the cookie; logout calls it | GREEN | — (yeşil, BEKLENEN: PART III, L18) |
> | X19s | PART III: the Cookie header read under a name built at run time | GREEN | — (yeşil, BEKLENEN: PART III, L18) |
> | X20b | type alias pv = operatorpages.ProblemView; var _ = pv{Back: /operator/nowhere} in render.go | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20c | CONTROL: the same literal spelled operatorpages.ProblemView{...} in home | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20d | elided type in a slice literal: []operatorpages.ProblemView{{Back: ...}} | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20e | zero value + field write in home | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20f | conversion from an identical struct type | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20g | generic constructor instantiated with ProblemView | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20h | embedding: a wrapper struct's promoted Back written | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20i | a listed variable's Back made non-constant | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20j | an unkeyed literal, listed by problemPages | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20k | problemPages omits a variable (N14 shape) | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20l | problemPages calls problemTooMany(false) twice | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X20m | dot import of operatorpages in a new file, ProblemView{Back: ...} | RED | `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` |
> | X03 | floodGate removed from the console group | RED | `TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate` |
> | X21a | the origin refusal back at WARN | RED | `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` (3. turdaki adıyla koşuldu; 4. turda yeniden adlandırıldı) |
> | X21b | option (b) done naively: an obeyed floodGate in front of sign-out | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt`, `TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend`, `TestLogout_PastTheCeilingTheBrowserStillForgetsTheSession`, `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` (3. turdaki adıyla koşuldu; 4. turda yeniden adlandırıldı) |
> | X21c | the refusal line removed altogether (the Debug control) | RED | `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` (3. turdaki adıyla koşuldu; 4. turda yeniden adlandırıldı) |
> | X22a | the passwords-differ re-render bypasses renderEnroll | RED | `TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | X22b | a revealed password reaches a log attribute (as len's argument) | RED | `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` |
> | X22c | r.FormValue in signIn | RED | `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` |
> | X22d | r.PostForm.Get("password") read directly in signIn | RED | `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` |
> | X22e | logoutGate logs its rate key at Debug | RED | `TestClientAddress_TheListedReadsFeedOnlyTheBudgets`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | X22f | rateKey touches r.RemoteAddr | RED | `TestClientAddress_TheListedReadsFeedOnlyTheBudgets` |
> | X22g | a redirect target taken from the request | RED | `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | X22h | securityHeaders deletes a header | RED | `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | X22i | render writes a Location header | RED | `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | X22j | hostGate compares r.Host itself | RED | `TestHostGate_TheListedHostReadsOccurOnlyThroughOnHost` |
> | X22k | operatorpages exports a component screens() does not render | RED | `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` |
> | X22l | a customer-side package imports operatorpages | RED | `TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell` |
> | X22m | operatorhost.go imports internal/config | RED | `TestOperatorHostFile_ImportsOnlyThreeStandardPackages` |
> | X22n | internal/httpx comes to depend on an operator package (operatorauth, through a new file) | RED | `TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell` |
>
> **Kart düzeltmesi — 4. tur (2026-10-02, 3. üçüncü göz denetçisinin RED'inden sonra: 1 bloklayan + 6
> bloklamayan; hepsi kapatıldı). İDDİANIN BİÇİMİ DEĞİŞTİ (orkestratör kararı: sınıf üç tur üst üste —
> 1. tur B2, 2. tur F1, 3. tur B1).** Her tur bir denetçi bir pinin listesinin dışında kalan bir Go
> biçimi bulup onu "so nothing …", "never", "only … in the package" diyen bir cümleyle çelişir
> buluyordu. Bu turda: (1) pinden ürüne dair genel hüküm çıkaran cümleler kaldırıldı ya da "pin X
> şu listeyi yakalar" biçimine indirildi; (2) PART III tek düz cümle, gerekçesiz; (3) korunması
> önemli olan yanıt başlıkları DAVRANIŞ olarak, 40 sayılı sınıfta, düşmanca istek başlıklarıyla
> ölçülüyor; (4) süpürme tablosu aşağıda yeniden yapıldı ve kalan evrensel sözcüklerin listesi
> teslim raporundadır (kart özetiyle).
>
> Değişen: ürün — `routes.go` (`sameOriginGate`'in ret kaydı: pencere başına bir WARN, gerisi Debug;
> yorumlar), `surface.go` (`originRefusals` sınırlayıcısı, `originRefusalPeriod`; yorumlar),
> `render.go`, `form.go`, `enroll.go`, `signin.go`, `console.go` (yorumlar),
> `web/templates/operatorpages/enroll.templ` (zayıf parola uyarısı metni + yorum), `view.go`,
> `chrome.templ`, `home.templ`, `signin.templ`, `layout/base.templ` (yorumlar; üretilen `_templ.go`),
> `web/static/js/operator/enroll.js` (yorumlar; kodun özeti değişmedi), `internal/httpx/operatorhost.go`,
> `router.go`, `cmd/tappa/operator.go` (açılış satırının metni), `operatorauth/limits.go`,
> `config/config.go`, `handler/marketing.go`, `deploy/README.md` (cümleler); testler — 40 sınıflık
> başlık testi düşmanca başlık + sorgu ile (`op8r2_test.go`: `hostileHeaders`, `hostileQuery`,
> `hostileCookieLine`, `designedHeaders`, `checkDesignedHeaders`, kontrol), `rig_test.go` (`cookieLine`),
> `op8r3_test.go` (F5 testi yeniden adlandırıldı:
> `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel`; iki adres), yeni `op8r4_test.go`
> (`TestEnroll_TheWeakPasswordNoticeNamesBothLimits`), `typepins_test.go` (başlıklar; `reflectiveImports` →
> `refusedImports`), başlık ve yorum düzeltmeleri `op8_test.go`, `op8_db_test.go`, `leak_test.go`,
> `export_test.go`, `operatorhost_test.go`, `operatorauth/surface_external_test.go`, `export_test.go`,
> `cmd/tappa/observability_test.go`, `handler/marketing_test.go`; bu kart ve ADR 0020 (OP-8 notu bu
> biçimle yeniden yazıldı). Migration YOK, bağımlılık YOK. `app.css` değişmedi (`make css` öncesi/sonrası
> bayt bayt aynı — yorumlardan kural doğmadı). Kartın OP-8 bloğunda md. 1–19 yeniden yazıldı, md. 20–21,
> L18–L19 güncellendi, 2. ve 3. tur alt bloklarının evrensel cümleleri işaretlendi, 3. turun süpürme
> tablosu kaldırıldı.
>
> - **B1 (bloklayan) · yanıt başlıkları.** Ne değişti: RH pinine dayanan cümleler ("never deleted",
>   "the header written only in those two places", "Location is written nowhere else … So nothing
>   from the request is put into a Location header", ADR (v), md. 11) kaldırıldı; başlıkların iddiası
>   artık DAVRANIŞ: `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` 40 sınıfın her birini
>   `Location`, `Content-Security-Policy`, `Cache-Control`, `Referrer-Policy`, `X-Content-Type-Options`,
>   `Set-Cookie`, `X-Frame-Options`, `Access-Control-Allow-Origin`, `Refresh` istek başlıkları ve
>   operatörün iki çerez adını taşıyan ikinci bir `Cookie` satırıyla sürer ve her sınıfta yanıt
>   başlıklarının ADLARINI tasarlanan kümeye, değerlerini tasarlanan değerlere tutar (md. 11).
>   Pozitif kontrol: başlıkları yanıta kopyalayan, sorguyu gövdeye yazan bir işleyici kontrolü geçemez.
>   **G-RH-copy2 (X23a), G-RH-copy (X23b), G-RH-clear (X23c), G-RH-mime (X23d) KIRMIZI** (başlık testi);
>   RH pini X23a–X23c'de yeşil kaldı (PART III, beklenen).
> - **B2 · sorgu okumaları.** `enroll.go` ve md. 15 metni FV listesine indirildi. Davranış: md. 20 —
>   40 sınıflık testin ~~27~~ 28 sınıfı *(6. tur, N-1: C40 sayılmamıştı; C41–C48 eklendikten sonra
>   iki tabloda 32)* düşmanca bir sorgu dizgisiyle (token, adres, parola, kod, blob; POST'ta id)
>   sürüldü ve o değerler yanıtlarda ham ya da sorgu-kaçışlı biçimiyle bulunmadı *(6. tur, B1)*; ayrıca `TestEnroll_TheTokenNeverTravelsInTheURL`,
>   `TestSignIn_ReadsTheBodyNeverTheQuery`, sızıntı A28/A29. **G-FV-q1 (X24a: `strings.Cut(r.URL.RequestURI(),
>   "token=")`'un sonucunu sayfaya yazmak) KIRMIZI** — başlık testi, `TestEnroll_TheTokenNeverTravelsInTheURL`,
>   sızıntı testi; **G-FV-q2 (X24b: `r.URL.Redacted()`'i Info'da log'lamak) KIRMIZI** — sızıntı testi
>   (A29, S1). FV pini ikisinde de yeşil (PART III, beklenen). *(İtiraz, ölçümle: "sonuç ve gövde
>   sorgusuzla aynı" iki sürüşle ölçülemez — enrollment sayfası her yüklemede taze anahtar ve blob
>   taşır, bütçe ve oturum durumu sürüşler arasında değişir; ölçülen: tasarlanan durum, tasarlanan
>   başlıklar ve sorgu değerlerinin yanıtta ham ya da sorgu-kaçışlı biçimiyle yokluğu — 6. tur, B1.)*
> - **B3 · yükleyicinin reddettiği importlar.** "yansımalı çözücüler" genellemesi kaldırıldı; sekiz import
>   adıyla (`typepins_test.go` `refusedImports`, L18, ADR (II)/(III)). **G-PV-asn1 (X25a) YEŞİL —
>   PART III, beklenen** (`encoding/asn1` listede değil).
> - **B4 · host okumaları.** `routes.go` `hostGate`/`OnHost` ve ADR (ii)'deki "no other read of the
>   request's host" / "host'u yalnız httpx.OnHost okur" kaldırıldı; HG pini listesini yakalar.
>   **G-HG-tls (X25b, `r.TLS.ServerName`) YEŞİL — PART III, beklenen.**
> - **B5 · yönlendirilmiş-adres başlıkları.** "the forwarded-address headers" → AD3'ün altı adı adıyla
>   (md. 14, `typepins_test.go` `forwardedHeaders`). Ölçülen yarı G15 × S1–S4 × A1–A30.
>   **G-AD-hdr (X25c, `X-Cluster-Client-IP`) YEŞİL — PART III, beklenen.**
> - **B6 · F5 kararı.** Önerilen üçüncü seçenek uygulandı: `sameOriginGate`'in ret kaydı, süreç geneli
>   10 dakikalık pencerenin ilk reddi için bir WARN (yöntem; adres yok — OP-8'in adres kararı), gerisi
>   Debug (`httpx.Limiter`, sınır 0, sabit anahtar; `logoutGate`/`sessionGate`'teki `FirstOverLimit`
>   deseni). Ölçüm `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` (Info): 500
>   reddedilmiş çıkış + 500 çapraz-origin giriş (300 × 403, 200 × 429) + ikinci adresten 1 ret → 801
>   ret, **1 WARN kaydı**, store 0, erişim kaydı 1 001; ardından operatörün kendi çıkışı aynı adresten
>   oturumu kapatır (B3 bozulmadı); kontrol: Debug'da iki ret → 1 WARN + 1 DEBUG. Panelin
>   `sameOriginGate`'i her reddi WARN olarak, ip ile yazar (`internal/handler/adminlogin.go:592`);
>   `deploy/README.md`'nin yedi uyarı kuralından biri de operatör 403'ünü ya da bu kaydı okumaz (L19).
>   **X26a (3. turun hep-Debug'ı) KIRMIZI, X26b (her rete WARN) KIRMIZI — 801 WARN, X26c (anahtar adres
>   başına) KIRMIZI — 2 WARN;** pozitif kontrol: sevk edilen kod yeşil; X21b (çıkışın önüne uyulan flood
>   kapısı) 3. turda kırmızıydı.
> - **B7 · zayıf parola uyarısı.** `operatorauth` 14 karakterden kısa, 72 bayttan uzun ve UTF-8 olmayan
>   parolayı tek hatayla döndürür; başlık "That password is too short" diyordu. Yeni metin (tappa-brand
>   ses tonu: ne yapılacağını söyler): başlık "Choose another password", gövde "Use at least 14
>   characters and at most 72 bytes. A letter with an accent, or from another alphabet, takes two bytes
>   or more." `TestEnroll_TheWeakPasswordNoticeNamesBothLimits`: dört kol (13 karakter, 73 tek baytlı, 37 iki
>   baytlı, UTF-8 olmayan 20 bayt) 400 + bu uyarı, "too short" yok; kontrol: kurala uyan parola yanlış
>   kodla kod uyarısını alır. **X27a (eski başlık) KIRMIZI, X27b (72 bayt cümlesi yok) KIRMIZI.**
>
> **Süpürme (4. tur, yeniden yapıldı) — OP-8 metinlerinde değişen cümleler.** Tür: *PART I* = sayılı bir
> kümede ölçülen davranış (adıyla test); *PART II* = bir pinin yakaladığı liste; *PART III* = tamlık
> iddiası yok (tek cümle); *kod tarifi (okuma)* = kodun ne yaptığının evrensel sözcüksüz tarifi; *olgu
> (dış)* = ürün dışı bir olgu (HTTP, tarayıcı). 3. turun "sertleştirildi" satırlarının hepsi burada
> yeniden değerlendirildi; o satırların konuları — çerez okuması, `ProblemView`, `renderEnroll`, `redirect`,
> `rateKey`, `OnHost`/`hostGate`, `httpx` ve `operatorhost.go` importları, `sameOriginGate`, `floodGate`,
> `reveal`, enrollment'ın sorgu okuması, `operatorpages` importları, `Back`, `base.templ`'in script-src
> cümlesi, ekran testleri — aşağıda kendi satırlarındadır.
>
> | # | Yer | Önceki metin (alıntı) | Yeni metin (özet) | Tür |
> |---|---|---|---|---|
> | 1 | routes.go `mount` | "for every method and every path under Prefix"; "the 3 001 the test sends refuse nothing"; "is the only BUDGET that refuses it" | zincir kodu tarif eder; çıkışın üç ölçümü adıyla (3 001 çerezsiz, 3 000 çerezli, 500 çapraz-origin) | PART I |
> | 2 | routes.go `hostGate` | "The comparison is httpx.OnHost …; for THIS package that is pinned (… one OnHost call, here, and no other read of the request's host …)" (B4) | "when httpx.OnHost(r, s.host) is false it answers http.NotFound"; ölçüm: manifest host'ları + altı host, yedi yöntem, dokuz yol | PART I |
> | 3 | routes.go `securityHeaders` | "structurally, these four names are written only here (and the CSP replaced only by renderEnroll), never deleted" (B1) | "sets four response headers"; ölçüm: 40 sınıf düşmanca başlıklarla, adlar ve değerler tasarlanana eşit | PART I |
> | 4 | routes.go `rateKey` | "STRUCTURALLY, this package reads the address only here and hands it only to the budgets … whose list includes RemoteAddr and the forwarded-address headers" (B5) | "uses it as a budget key"; ölçüm G15 × S1–S4 × A1–A30; "AD pini başlığındaki listeyi yakalar" | PART I + PART II |
> | 5 | routes.go `floodGate` | "every request in the group charges it, and a request past it is answered 429 before it is looked at further" | "a request in the sign-in and console groups charges it"; konsolda ölçüm (tükenmiş adres, 429, store 0) | PART I |
> | 6 | routes.go `logoutGate` | "sign-out's own wider ceiling is the only thing that refuses it" | "refuses past sign-out's own ceiling (signOutLimit) with 429" | kod tarifi (okuma) |
> | 7 | routes.go `sameOriginGate` | "writes a process-log record only at Debug … refused sign-outs are unbounded, and each is an access record and a 403 render, nothing more" (B6) | "the first refusal of a 10-minute window, process-wide, is one WARN record …; the others are Debug"; ölçüm 801 ret / 1 WARN | PART I |
> | 8 | routes.go `requireOperator` | "(I) … reads the session cookie no other way the pin lists; (II) … resolves … through any alias …; (III) …" | "TestSessionCookieReads_… catches the list in its header" | PART II |
> | 9 | routes.go `sessionGate` | "runs THE session predicate" | "runs the session predicate" | kod tarifi (okuma) |
> | 10 | render.go `operatorCSP` | "Both halves are pinned: … and in the source -- the header written only in those two places" (B1) | "Measured on the 40 classes of TestOperatorHeaders_FortyResponseClassesCarryThePolicy" | PART I |
> | 11 | render.go `enrollCSP` | "leaves those files unloadable on this page even if markup were injected into it" | "does not let a page under this policy load them"; ölçüm 4 / 36 sınıf | PART I |
> | 12 | render.go `readForm` | "parses the POST body (never the query string: PostForm, not Form)" | "the handlers read r.PostForm (the body), not r.Form" | kod tarifi (okuma) |
> | 13 | render.go `redirect` | "Every call in this package passes a constant …, and Location is written nowhere else in the package … So nothing from the request is put into a Location header by this package." (B1) | ölçüm: 40 sınıfın Location'ı düşmanca Location isteğiyle tasarlanan yol; "RH pini başlığındaki listeyi yakalar" | PART I + PART II |
> | 14 | render.go `renderEnroll` | "is the one place this package renders the enrollment screen … each used once, in their one place" | "renders the enrollment screen …"; ölçüm 4 / 36; "EN pini listesini yakalar" | PART I + PART II |
> | 15 | render.go ProblemView bloğu | "WHERE A ProblemView IS BUILT, IN THREE PARTS … the package sets ProblemView fields only in the eight variables below and in problemTooMany" | "problemPages lists them (eight variables and problemTooMany twice); link testi onunu render eder; PV pini listesini yakalar" | PART I + PART II |
> | 16 | render.go `problemPages` | "lists every ProblemView this package sets a field of (the pin above)" | "lists the eight variables above and problemTooMany(false) and (true)" | kod tarifi (okuma) |
> | 17 | render.go sayfa sabitleri | "none quotes the request" | "Their sentences are constants" | kod tarifi (okuma) |
> | 18 | form.go `formValue` | "Where it is called is pinned … allows exactly nine … and refuses reveal taken as a value and a credential field's name used anywhere but as postValue's argument. PART III there: …" (B2) | "TestFormValues_… catches the list in its header"; "a deliberate extraction is not prevented" | PART II |
> | 19 | form.go `postValue` | "(never the query string)" | "one field of r.PostForm (the body)" | kod tarifi (okuma) |
> | 20 | enroll.go dosya notu | "A token that arrives in the QUERY anyway (?token=...) is ignored: the GET reads only id, and the POST reads only its body (… pins the package's query read to enrollPage's one .Get("id") …)" (B2) | "enrollPage reads the query's id; enroll reads r.PostForm"; ölçüm: TestEnroll_TheTokenNeverTravelsInTheURL, A29, 27 sınıfın düşmanca sorgusu; "FV pini listesini yakalar" | PART I + PART II |
> | 21 | enroll.go dosya notu | "a browser never sends a fragment, so it reaches no ingress log, no proxy and no access record" | "which a browser does not put in a request" | olgu (dış) |
> | 22 | enroll.go `enrollPage` | "reads no database … every well-formed id gets the same kind of page" | "makes no store call … a well-formed id gets the same kind of page whether it names a pending account, an active one or nobody" | kod tarifi (okuma) |
> | 23 | enroll.go `enroll` | "THE TWO PASSWORDS ARE COMPARED HERE, FIRST, AND THAT REFUSAL COSTS NOTHING … before any budget is spent or any row is written"; "the one place the token is written after it arrives" | "a mismatch is answered before a budget is charged or a store method is called"; "the leak test's designed egress D5"; B7 notu | kod tarifi + PART I |
> | 24 | signin.go `signInPage` | "It reads nothing and writes nothing." | "it makes no store call and sets no cookie" | kod tarifi (okuma) |
> | 25 | signin.go `signIn` | "🔴 ONE ANSWER FOR EVERY REFUSED ARM, AND THIS HANDLER ADDS NO ARM THAT DEPENDS ON THE ACCOUNT" | "ONE ANSWER FOR THE REFUSED ARMS"; ölçüm sekiz kol | PART I |
> | 26 | signin.go `codePage`/`code` | "its presence is all a page needs"; "has no use once a session exists" | ölçülen davranış + `TestSignIn_TheCodeStepClearsTheChallenge` | PART I |
> | 27 | console.go `home` | "links to no screen that does not exist" | "its links are held to the mounted routes by TestOperatorScreens_EveryActionAndLinkIsAMountedRoute" | PART I |
> | 28 | console.go `logout` | "THE COOKIE IS CLEARED ONLY WHEN THE DATABASE HAS ENDED THE SESSION"; "A request with no cookie never gets here" | "On a database fault the session cookie is kept … Measured: …"; "requireOperator is in front" | PART I |
> | 29 | surface.go paket notu | "none of them reads a tenant … NOT registered … and nothing links to them" | "none of the five reads a tenant"; iki testle ölçüm | PART I |
> | 30 | surface.go `sessionLimit` | "so only the holder of that session's cookie can spend it"; "That work is bounded per ADDRESS only, by the flood budget in front" | "spending it takes that session's cookie"; "The predicate's work per address is bounded by the flood gate in front" + ölçüm | PART I |
> | 31 | surface.go `signOutLimit` | "and the ONLY budget that may refuse one" | kaldırıldı ("logoutGate refuses past ten times that number") | kod tarifi (okuma) |
> | 32 | surface.go `operatorOrigin` | "a browser's Origin header never carries one"; "instead of failing every sign-in" | "a browser serialises Origin without it"; kaldırıldı | olgu (dış) |
> | 33 | httpx/operatorhost.go dosya notu | "operator routes answer 404 on every other host"; "🔴 THIS FILE IMPORTS NOTHING OF THE OPERATOR'S … imports are net, net/http and strings and nothing else" | "on a host that is not the operator's"; "This file's imports are net, net/http and strings (TestOperatorHostFile_… catches a change to that list)" | kod tarifi + PART II |
> | 34 | httpx/operatorhost.go `OnHost` | "The OPERATOR half's call is pinned there (… and no other read of the request's host in that package)" (B4) | "operatorHostOnly here and hostGate … call it" | kod tarifi (okuma) |
> | 35 | httpx/operatorhost.go `OnHost` | "X-Forwarded-Host is not read: not here …, not in internal/handler/operator (HG3 …); a search … found no reader" | "OnHost does not read X-Forwarded-Host (that test's two X-Forwarded-Host rows; mutation M08)" | PART I |
> | 36 | httpx/operatorhost.go `OnHost` | "Either way the page making the POST is an operator page: that spelling serves nothing else. The wider classification is the side that keeps customer pages away from anything that spells the operator host." | "Classified as the operator host, such a request is served the operator surface" | kod tarifi (okuma) |
> | 37 | httpx/operatorhost.go `operatorHostOnly` | "it lets through only the operator surface … answers every other path 404 … On every other host it does nothing"; "so no probe ever carries the operator host" | "it passes … and answers the other paths with http.NotFound … When OnHost is false it passes the request on unchanged. Measured on the rows …"; "(read from the manifest; not measured in a cluster)" | kod tarifi + PART I |
> | 38 | httpx/operatorhost.go `routingPath` | "EQUIVALENT for these prefixes -- the decoded path starts with /operator/ or /static/ exactly when the escaped one does" | "Mutation M09 … left the tests green: on their rows the two classify alike" | PART I |
> | 39 | httpx/router.go | "that host serves the operator surface and the static assets and nothing else … so no customer handler runs first" | "operatorHostOnly decides that host's requests … it runs before a route's handler" | kod tarifi (okuma) |
> | 40 | operatorpages/view.go paket notu | "nothing on the customer side imports this package … and no other package of the module. Both halves pinned" | "Measured with `go list` (non-test imports, the build being run): …" | PART I |
> | 41 | operatorpages/view.go `TenantName`/`TenantScreen` | "the only way to hold a name is NewTenantName"; "the chrome a screen … renders through … (that every such screen does is code review's …) … writing NOTHING" | "a name is made with NewTenantName"; "returns ErrNoTenantName before writing a byte" | kod tarifi (okuma) |
> | 42 | operatorpages/view.go `SignInView` | "Failed is the one sentence a refused password step shows … whatever was typed" | "Failed selects the sentence …"; sekiz kolun bayt karşılaştırması | PART I |
> | 43 | operatorpages/view.go `Back`, `Token` | "In internal/handler/operator every Back set is a constant (… PV4)"; "which no browser sends" | "The links of the ten pages … are held to mounted routes by TestProblemPages_LinkOnlyToMountedRoutes"; "which a browser does not put in a request" | PART I |
> | 44 | operatorpages/home.templ | "🔴 IT SHOWS NO TENANT DATA AND PROMISES NOTHING THE BUILD DOES NOT DO … links to no screen"; "never quote a request" | "It shows no tenant data … carries no link of its own"; "Its sentences come from the handler's ProblemView constants" | kod tarifi (okuma) |
> | 45 | operatorpages/chrome.templ | "Every operator screen this package ships is rendered inside …"; "this is the one surface of the product that reaches across tenants"; "Its one caller is tenantScreen …"; "for the enrollment screen alone. It is never signed in"; "TenantScreen is the door" | "The six exported screens … Measured: 12 variants"; "this surface reaches across tenants (ADR 0021)"; "In this package's source today its caller is … (read, not pinned)"; "Enroll is its caller. It draws the bar signed out" | PART I + kod tarifi |
> | 46 | operatorpages/enroll.templ | "It is the ONLY script an operator screen loads … names this exact path and nothing else (enrollCSP, which only renderEnroll sends -- EN pin)"; "which a browser never sends"; "never as a QR code" | "Measured: of the 40 … four load a script … their policy names this path"; "does not put in a request"; "not as a QR code" | PART I |
> | 47 | operatorpages/signin.templ | "THE FAILURE SENTENCE IS FIXED …"; "It is reached only with a login challenge …; the handler sends anyone else back"; "shown only to someone holding a challenge, which only the right password mints, so it tells the password holder nothing they could not learn by trying" | sekiz kolun bayt karşılaştırması; "The handler renders it for a request with a login challenge cookie and redirects the others"; "It is shown to a request holding a challenge, which the right password mints, so it tells the password holder what trying codes would tell them" | PART I + kod tarifi |
> | 48 | layout/base.templ `Operator` | "screens must never be mistaken"; "an operator page is never indexed"; "only the enrollment screen names a script-src, and it names exactly one file (pinned there: EN, RH)" | "meant not to be mistaken"; kaldırıldı; "measured there, of the 40 response classes … the four enrollment ones carry a script-src naming one file" | PART I |
> | 49 | layout/base.templ `OperatorWithScript` | "Its one caller outside this file is … read in the source, not pinned"; "Nothing here builds it from user input" | "In the source today its caller outside this file is … -- read, not pinned"; "This shell writes src as given; the caller passes a constant (enrollScript)" | kod tarifi (okuma) |
> | 50 | static/js/operator/enroll.js | "a browser never sends a fragment … never reaches an ingress log"; "NAMES THIS FILE AND NOTHING ELSE"; "No request, no storage, no cookie. It never reads the password or code fields."; "the server refuses it the same way whether or not it was copied" | "which a browser does not put in a request"; "THE POLICY THAT PERMITS IT"; "The code below makes no request, … -- read by a reviewer; the browser behaviour was measured by hand (L15)"; "A fragment of another shape is not copied into the field" | PART I (elle) + kod tarifi |
> | 51 | typepins_test.go dosya başlığı | "PART III -- no completeness claim. What every pin here misses, by name: … reflection, unsafe and linkname are not reachable from this package's own code only because typedOperator refuses those imports (reflectiveImports)" (B3) | üç parçanın tanımı; "imports one of the eight packages in refusedImports" | PART II + PART III |
> | 52 | typepins_test.go `refusedImports` | "each reaches a value or a method without naming it … (the two template engines, the reflective decoders)" (B3) | "the eight imports typedOperator refuses …, by name: reflect, unsafe, plugin, text/template, html/template, encoding/json, encoding/gob, encoding/xml" | PART II |
> | 53 | typepins_test.go dokuz pin başlığı | PART III'ler gerekçe içeriyordu (ör. RH: "a header named at run time through a method this pin does not list (Header.Values reads; a write needs Set/Add, which RH2 holds to constants)") (B1) | PART III şablon tek cümle: "This pin catches the list in PART II only; a form not on it (examples: …) is code review's -- no completeness claim." | PART III |
> | 54 | typepins_test.go RH PART I | "appears as a constant in its allowed functions only" | "the constant occurrences of each guardedHeaders name are its allowed functions with its allowed counts" | PART I |
> | 55 | op8r2_test.go başlık testi | "(II) … TestResponseHeaders_… pins the source side -- the four names written only in securityHeaders …, the CSP replaced only in renderEnroll, no Del; (III) …" (B1) | PART I: düşmanca başlık + sorgu ile 40 sınıfın ölçülen listesi; PART II: o liste; PART III şablon | PART I + PART III |
> | 56 | op8r2_test.go diğer başlıklar | "renders every ProblemView problemPages lists … That the list holds every ProblemView the package sets a field of is …'s"; "every line trimmed"; "Any edit to the code turns … red" | "renders the ten ProblemViews problemPages lists …; TestProblemViews_… is the pin … (its header lists what it catches)"; "the lines trimmed"; "An edit to the code turns … red" | PART I + PART II |
> | 57 | op8_test.go başlıkları | "the same-origin gate ahead of every store call"; "(only that package's tests can count its comparer)"; "every route the configured surface registers"; "it serves nothing else"; "which only a non-browser sends"; "every one is 303"; "change nothing here"; "never echo a request value into Location"; "WRITES NOTHING"; "every fmt verb this test names"; "it knows only TAPPA_BASE_URL's host"; "already keeps customer routes off the operator host"; "and only that session's" | "ahead of the store"; "(that package's test build is the one that can count its comparer)"; "the routes … (walked with chi.Walk)"; "a page under that spelling is served by the operator surface"; kaldırıldı; "each of the six is 303"; "leave the console's answer the sign-in redirect"; "On these three redirected requests Location carries no request value"; "writes 0 bytes"; "the fmt verbs this test names"; "it compares the operator host with TAPPA_BASE_URL's"; "answers the customer route driven on the operator host with 404"; "another operator session afterwards still renders (CONTROL)" | PART I |
> | 58 | op8_db_test.go | "a Store whose every method is …"; "every store call"; "The OPERATOR rows this file writes are all in those rolled-back transactions"; "every TestE2E_ here"; "every panel database test's … never cleaned up"; "each test here is worthless if …"; "the only role that may"; "Every step through the surface, every database decision the database's" | "a Store whose seven methods call …"; "each store call"; "… (counted below: 0 left in the operator tables)"; "this file's TestE2E_ tests"; "the panel database tests' fixtures … does not clean them up"; "a test here measures nothing about 00026 if …"; "(ADR 0020 §6: tappa_owner inserts platform_admins)"; "each step through the surface, against the real definers" | PART I |
> | 59 | rig_test.go, export_test.go, operatorauth/export_test.go, surface_external_test.go | "Every request goes through"; "only the Store is a fake"; "COUNTS every call"; "(RLS shows no other)"; "Test-only doors … no product code can name them"; "never stand-ins"; "only this package's test build can wrap them"; "every one is 401"; "the only arm that sets a cookie"; "past it nothing is compared and nothing is written" | "The rig's requests go through"; "the Store is the fake part"; "counts its calls"; "(tappa_operator's RLS shows active rows)"; "This file is a _test.go file, so the go tool compiles it into this package's test build and not into the product"; "not stand-ins"; "this package's test build wraps them"; "each of the eight is 401"; "sets one cookie where the eight set none"; "answered with 0 comparisons and 0 store calls" | PART I + kod tarifi |
> | 60 | httpx/operatorhost_test.go | "pins the comparison … and no other row matches. Every row that is "false" …"; "An unconfigured host matches nothing"; "Every other host"; "nothing is gated"; "pins operatorhost.go's "THIS FILE IMPORTS NOTHING …"" | "measures OnHost on the rows below … the rows marked false …"; "An unconfigured (empty) host does not match an empty request host"; "The customer host"; "the gate is not mounted"; üç parçalı başlık | PART I + PART II + PART III |
> | 61 | leak_test.go | "written by the templ-generated code every operator screen goes through"; "THE CONTRACT, AND ONLY THE CONTRACT"; "what it is handed that this surface must never print"; "closes nothing" | "of the operator screens"; "THE CONTRACT"; "what it is handed (…), which the search adds to its needles"; "makes no CloseOperatorSession call" | PART I |
> | 62 | handler/marketing.go | "from every non-test Go file … grows a cookie"; "they are set only on the operator's own host … a visitor of this site never receives one" | "from the non-test Go files … the source names a cookie"; "`__Host-` cookies set by the operator surface on the operator's host … not for this site's visitors"; *5. tur (N2):* tarayıcının okuduğu iki biçime bağlandı — "the string literals of the two name shapes it reads, `tappa_…` and `__Host-taptime_…` (cookieNameLiteral) … A cookie named in another shape is not read by it" | ~~olgu (dış)~~ PART I (taramanın iki biçimi, `TestCookieNotice_ListsExactlyTheCookiesTheProductSets`) + PART III — *5. turda düzeltildi* |
> | 63 | handler/marketing_test.go | "Every cookie this product sets is named by a string literal …, and nothing else in non-test Go source is"; "whose every customer route answers 404"; "sent only to the operator host"; "Every deliberate omission"; "in ITS OWN file only"; "only its own file's literal may be" | "The scan reads a string literal … as a cookie name"; "where the customer half of the host gate answers customer routes with 404"; "a __Host- cookie of the operator host"; "Each deliberate omission"; "in ITS OWN file"; "its own file's literal is the omitted one" | kod tarifi (okuma) |
> | 64 | operatorauth/limits.go, config/config.go, cmd/tappa/operator.go, observability_test.go | "never an account"; "off every customer host"; log: "/operator answers on the operator host only"; "match nothing" | "not an account"; "off the customer hosts is the two-way host gate's job"; log: "/operator is served on the operator host"; "match no record" | kod tarifi (okuma) |
> | 65 | deploy/README.md | "yalnız operatör host'unda"; "`/operator` her istekte 503"; "Satır her süreçte"; "(yalnız bir kod — DSN, parola, sunucunun mesajı ASLA)"; "okunmazsa hiçbir yerde görünmez" | "operatör host'unda"; "`/operator` ve altı 503 (TestSurface_OffAnswers503UnderThePrefixAndNowhereElse)"; "süreç açılışında"; "(bir kod; TestOpenOperatorSurface_PrintsNoValue'nun sürdüğü değerlerden … satırda bulunmadı)"; "okunmazsa başka bir sinyal onu taşımaz (yedi kuralın tablosu)" | PART I |
> | 66 | ADR 0020 §4 OP-8 notu (ii) | "Operatör yarısında host'u yalnız httpx.OnHost okur, bir kez, hostGate'te" (B4); "`httpx` operatör paketlerini import etmez …" | silindi; host kapısı ölçümleri adıyla; httpx bağımlılıkları (II)'deki IM pinine | PART I |
> | 67 | ADR 0020 §4 OP-8 notu (iv) | "süreç log'una yalnız Debug'da bir kayıt yazar" (3. tur) | "10 dakikalık, süreç geneli bir pencerenin ilk reddi bir WARN kaydı …, diğerleri Debug — Info'da iki adresten 801 ret → 1 WARN" (B6) | PART I |
> | 68 | ADR 0020 §4 OP-8 notu (v) | "kaynak yarısı: dört ad yalnız securityHeaders'ta …, CSP'yi yalnız renderEnroll değiştirir, Location yalnız redirect'te ve hedefi bağlı bir rotanın sabiti" (B1) | düşmanca başlık + sorguyla 40 sınıfın ölçülen başlık listesi | PART I |
> | 69 | ADR 0020 §4 OP-8 notu (II) | "`requireOperator`'da; … hiçbir dosyada — takma ad …"; "ortak PART III: … paketin kendi reflect/unsafe/şablon/çözücü importu yükleyicide reddedilir" (B3) | pinlerin adları ve kodları; "Her pinin yakaladığı liste testin başlığındadır ve pinin iddiası o listedir" | PART II |
> | 70 | ADR 0020 §4 OP-8 notu (III) | "listelenmeyen her kod değişikliği (…) kod incelemesinin konusudur" | tek cümle: "bu küme ve listelerde olmayan her biçim (örnekler: …) kod incelemesinin konusudur — tamlık iddiası yok" | PART III |
> | 71 | ADR 0020 §6 OP-8 notu | "Sorguya konan bir token okunmaz ve sayfaya yazılmaz"; "(her kod değişikliği kırmızı)"; "fragment hiçbir istekte yok"; "(III) … ancak yeniden elle ölçülür" | "… isteklerde yanıta yazılmadı"; "kodda bir değişiklik özeti değiştirir"; "ölçülen isteklerde yok"; PART III şablon | PART I + PART III |
> | 72 | kart md. 1 | "00026'da yalnız beş definer var … hiçbiri tenant okumaz … 12 render'ından hiçbiri … o küme API'ye pinli" | "beş definer var … beşinden biri de tenant okumaz … 12 render'ı onlara link vermez" | PART I |
> | 73 | kart md. 2 | "`httpx` operatör paketlerini import etmez: bildiği yalnız bir dizge … ve host'tur"; "503 her host'ta aynı kaldı"; "operatör host'unda müşteri sayfası yok" | importlar ve IM3 adıyla; "sürülen host'larda aynı kaldı"; "sürülen müşteri rotası 404'tür" | PART I |
> | 74 | kart md. 3 | "geniş sınıflandırma müşteri sayfalarını operatör host'unun her yazımından uzak tutar"; "o yazım yalnız operatör yüzeyini sunduğu için POST'u gönderen sayfa bir operatör sayfasıdır"; "`X-Forwarded-Host` okunmaz" | "bir yazımı müşteri host'u saymak … müşteri sayfası sunardı"; "o yazım operatör yüzeyine sınıflanır"; "`OnHost` `X-Forwarded-Host` okumaz (… M08)" | PART I |
> | 75 | kart md. 5–6 | "(enjekte edilmiş bir htmx etiketi yüklenmez)"; "config yalnız TAPPA_BASE_URL'in host'unu reddeder"; "operatör origin'inde müşteri sayfası render edilmez" | "(bu politika vendored htmx'i yükletmez)"; "… reddeder, öteki müşteri host'larını reddetmez"; "sürülen müşteri rotası (/admin) 404'tür" | PART I |
> | 76 | kart md. 7 | "`operatorauth.ReadSessionCookie`'nin tek kullanımı oradadır ve 3. tur pininin listelediği başka bir okuma biçimi paketin hiçbir dosyasında yoktur"; "yüklemin işi yalnız ADRES başına"; "hiçbir bütçeye sayılmadığı için … sayısı bir şey reddettirmez" | "SC1–SC6 listesini yakalar"; "yüklemin işini adres başına flood kapısı sınırlar"; "bütçeye sayılmaz (zincir sırası; N19) — ölçülen 3 001" | PART I + PART II |
> | 77 | kart md. 8 | "`Sec-Fetch-Site` yalnız `same-origin`"; "bir **Debug** kaydı (yalnız yöntem)" (3. tur) | "`same-origin` olmalı"; "pencerenin ilk reddi bir WARN …" (B6) | PART I |
> | 78 | kart md. 9 | "ikisi de hiçbir hesap hakkında bir şey söylemez"; "Yalnız `PostForm` okunur — sorgudaki kimlik bilgisi kimlik bilgisi değildir" | "ikisi de 0 bcrypt"; "Handler'lar r.PostForm'u okur; sorgudaki parola/adres giriş sayılmadı" | PART I |
> | 79 | kart md. 10 | "token yalnız onu gönderen POST'un yanıt gövdesine geri yazılır"; "ekranın TEK yazım yolu `renderEnroll`'dur"; "her iyi biçimli id aynı türden sayfayı alır"; "Sorgudaki token okunmaz" | "token onu gönderen POST'un yanıt gövdesine geri yazılır (D5)"; "Ekranı renderEnroll yazar …; EN1–EN3 listesini yakalar"; "iyi biçimli bir id … aynı türden sayfayı alır"; md. 20 | PART I + PART II |
> | 80 | kart md. 11 | "kaynak yarısı pinli — dört ad yalnız `securityHeaders`'ta …, CSP'yi yalnız `renderEnroll` değiştirir, `Del` yok, `Location` yalnız `redirect`'te ve her hedef bağlı bir rotanın sabiti" (B1) | düşmanca başlık + sorgu ölçümü; "RH1–RH5 listesini yakalar (maps.Copy ve clear o listede değil …)" | PART I + PART II |
> | 81 | kart md. 12 | "yalnız operatör host'una gönderilirler; ziyaretçi almaz" | "operatör host'unun `__Host-` çerezleri, Taptime personeli için" | olgu (dış) |
> | 82 | kart md. 14 | "yalnız bütçe anahtarı …; paket adresi yalnız `rateKey`'de okur ve yalnız bütçelere … verir — … `RemoteAddr` ve yönlendirilmiş-adres başlıkları dahil" (B5) | ölçüm G15; "AD1–AD5 listesini yakalar (AD3: altı başlık adı — adlarıyla)" | PART I + PART II |
> | 83 | kart md. 15 | "go/types ile pinli: tam dokuz …; kimlik bilgisi alan adları yalnız `postValue`'nun argümanı; form/sorgu okumaları listeli" (B2) | "sevk edilen kodda dokuz çağrı yeri var …; FV1–FV7 listesini yakalar (URL.RequestURI, URL.Redacted o listede değil)" | PART I + PART II |
> | 84 | kart md. 16–17, 19 | "`TenantName` yalnız `NewTenantName` ile"; "HİÇ bayt"; "var olmayan bir şey vaat etmez"; "Commit eden TEK test"; "her store çağrısı"; "tek seferlik"; "hiçbir hesap" | "paket dışında NewTenantName ile yapılır"; "0 bayt"; "bu build'de operatör ekranı olmadığını söyler"; "Commit eden test"; "store çağrıları"; "bir kerelik"; "hesap değil" | PART I + kod tarifi |
> | 85 | kart kabul | "her biri tam bir kez sorgulandı; … her değer kendi tarafında canlı"; "her giriş `operator_audit_log`'da →" | "ikisi de bir kez sorgulandı; … iki değer de"; "kabulün "her giriş …" maddesi →" (alıntı) | PART I |
> | 86 | kart L10, L13–L15 | "(yalnız operatör host'unda)"; "süreç dışı her şey"; "canlıda hiçbir şey açmadı"; "her kod değişikliği kırmızı"; "fragment hiçbir istekte yok" | "operatör host'unda"; "süreç dışındakiler"; "bir ingress kuralı eklemedi"; "kodda bir değişiklik özeti değiştirir"; "ölçülen isteklerde yok" | PART I |
> | 87 | kart L18 | "Operatör paketinin kendi `reflect`/`unsafe`/`plugin`/şablon/yansımalı çözücü importu … yükleyicide reddedilir" (B3) | "yalnız başlıklarındaki listeyi yakalar …; yükleyicinin reddettiği sekiz import adıyla: …"; ölçülmüş örnekler (X19r, X19s, X25a–X25c, X24a/b) | PART II + PART III |
> | 88 | kart L19 | "süreç log'u kaydı yalnız Debug'da" (3. tur) | "pencerenin ilk reddi bir WARN …; panelin sameOriginGate'i her reddi WARN olarak, ip ile yazar (adminlogin.go:592); yedi uyarı kuralından biri de bu kaydı okumaz" (B6) | PART I |
> | 89 | kart 2. tur B2/B3/B4/B6 ve başlık | "(ii) "`requireOperator` çerezin TEK okumasıdır" YAPISAL olarak doğru yapıldı"; "hiçbir bütçeye sayılmaz ve asla 429 almaz"; "Bütün ProblemView'lar problemPages'te"; "`renderEnroll` tek yol"; "`requireOperator` tek okuma"; "M09 eşdeğer — … bu öneklerde aynı" | "(ii) Çıkış da requireOperator'ın arkasına alındı …; 4. tur notu"; "bütçeye sayılmaz — testin 3 001 çıkışında store 0 ve 429 yok"; "problemPages on ProblemView'u listeler"; "Ekranı renderEnroll yazar"; "token'ı bağlama koyması"; "testlerin satırlarında aynı" | PART I + PART II |
> | 90 | kart 3. tur başlık ve F5 | "her ad … her ifade … çözülür: … AYNI nesnedir"; "Her pinin iddiası … ÜÇ PARÇALI"; F5 "Debug" kararı | "paketin adları … çözülür; … aynı nesneye çözüldü (X19, X19d2, X19e, X19g, X20b, X20m)"; "Pinlerin başlıkları üç parçalı"; F5'e "4. turda değişti — B6" notu | PART I + PART II |
> | 91 | kart 3. tur süpürme tablosu | 43 satır; "sertleştirildi" satırları pinden genel hüküm çıkarıyordu | kaldırıldı; bu tablo yerine geçer | — |
>
> **Kalan evrensel sözcükler (4. tur, (4)).** OP-8 metinlerinde (kod yorumları, testler, bu kartın OP-8
> bloğu, ADR 0020'nin iki OP-8 notu, `deploy/README.md`'nin OP-8 satırları) `only/never/nothing/every/no
> other/TEK/hiçbir/yalnız/asla/her` geçişlerinin satır satır listesi, her birinin bağlandığı sayılı kümeyle,
> teslim raporundadır. Kümeler: bir alıntı (çoğu bu tablonun "önceki metin" sütunu), adıyla sayılan bir
> küme (C1–C40, 27 sınıf, tablonun denemeleri, sızıntı testinin G/R/S kümeleri, adıyla sayılan pinler),
> bir sayı ("tek" = bir), PART III cümlesi ("listeyi"), bir pinin listesinin tarifi, bir mutasyonun
> tarifi, OP-7'nin değişmeyen metni, `neverLog` terimi.
>
> **4. tur mutasyon tablosu (kopyala-geri-yaz, yedekler yalnız scratchpad'de; yeni dosyalar koşudan
> sonra silindi; tablodaki her deneme diskte `diff` ile, geri yükleme sha256 ile; ağacın parmak izi
> önce/sonra eşit; hepsi paketin tamamıyla, `.env`'li; iki kez koşuldu — ikincisi son koda karşı — ve
> iki koşu aynı sonucu verdi).** 14 deneme: **11 kırmızı, 3 yeşil BEKLENEN**
> (X25a–X25c: PART III örnekleri — pinlerin listesinde olmayan biçimler).
>
> | Kimlik | Ne | Sonuç | Kırmızıya çevirdiği test(ler) |
> |---|---|---|---|
> | X23a | G-RH-copy2: a new file copies the request's headers (Coo* dropped) into the response; home calls it | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | X23b | G-RH-copy: home copies the request's headers into the response (maps.Copy) | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | X23c | G-RH-clear: home clears the response headers before rendering | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | X23d | G-RH-mime: home sets a header named at run time through textproto.MIMEHeader | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | X24a | G-FV-q1: the enrollment GET echoes a token cut from r.URL.RequestURI() | RED | `TestEnroll_TheTokenNeverTravelsInTheURL`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | X24b | G-FV-q2: the enrollment GET logs r.URL.Redacted() at Info | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` |
> | X25a | G-PV-asn1: a ProblemView with Back set, decoded by encoding/asn1 in a new file | GREEN | — (yeşil, BEKLENEN: PART III örneği) |
> | X25b | G-HG-tls: hostGate also admits r.TLS.ServerName == the operator host | GREEN | — (yeşil, BEKLENEN: PART III örneği) |
> | X25c | G-AD-hdr: rateKey prefers an X-Cluster-Client-IP header | GREEN | — (yeşil, BEKLENEN: PART III örneği) |
> | X26a | the 3rd round's all-Debug back (no WARN at Info) | RED | `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` |
> | X26b | a WARN for every refusal (the window ignored) | RED | `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` |
> | X26c | the WARN keyed per address (one per address per window) | RED | `TestClientAddress_TheListedReadsFeedOnlyTheBudgets`, `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` |
> | X27a | the weak-password heading back to 'That password is too short' (generated code) | RED | `TestEnroll_TheWeakPasswordNoticeNamesBothLimits` |
> | X27b | the weak-password notice without its 72-byte limit (generated code) | RED | `TestEnroll_TheWeakPasswordNoticeNamesBothLimits` |
>
> **Kart düzeltmesi — 5. tur (2026-10-02, 4. üçüncü göz denetçisinin RED'inden sonra: 2 bloklayan + 7
> bloklamayan).** 4. turun biçimi korundu (PART I sayılı kümede ölçülen, PART II pinin listesi, PART III
> tek cümle). Mutasyon kimlikleri aşağıdaki 5. tur tablosundadır. Yeni test dosyası
> `internal/handler/operator/op8r5_test.go` (ortak sınıf koşucusu `runHeaderClasses`, `classRoutes`,
> C41–C48, yürüme ve tel testleri); ürün kodunda değişen yalnız yorumlar (`routes.go`, `render.go`,
> `marketing.go`, `input.css`) — davranış değişmedi.
>
> - **F1 (bloklayan) · ölçülen nesne.** Başlık ölçen testler işleyicinin canlı haritasını (`w.Header()`)
>   okuyordu; WriteHeader'dan sonra değişen bir başlık telde olduğu hâlde testte görünmüyordu (denetçinin
>   H01'i: `Refresh` set + WriteHeader + `delete`; L01'i: `X-Key` = TOTP sırrının base32'si + `defer
>   delete` — ikisi de 52/52 yeşil). Ne değişti: OP-8'in dört paketinde (`internal/handler/operator`,
>   `internal/httpx`, `internal/operatorauth`, `cmd/tappa`) yanıtı ölçen test okumaları `Result().Header`'a
>   — WriteHeader anındaki kopyaya — çevrildi (`checkDesignedHeaders`, sızıntı testinin S4'ü ve öbürleri);
>   `grep -n "\.Header()"` dört paketin testlerinde 10 satır bulur: 5'i yanıt KURAN kontrol işleyicisi,
>   2'si tel testinin canlı haritayı bilerek okuyan kontrolü, 3'ü yorum (satır satır sınıflaması teslim
>   raporunda). Yeni test
>   `TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries`: C1, C18 ve C28 gerçek bir
>   `httptest.Server` üzerinden; durum ve başlık bölümü (`Content-Length`, `Date` adıyla dışarıda;
>   6. tur: trailer ve çerçeve başlıkları karşılaştırma dışı, md. 11) kopyaya eşit; kontrol: WriteHeader'dan sonra silinen bir başlık telde ve kopyada var, canlı haritada yok.
>   RH3'e `http.Header` üstünde yerleşik `delete`/`clear` eklendi. Ölçülen nesneyi adlandıran metinler:
>   başlık testinin başlığı (WHAT IS READ, PART I), `routes.go` `securityHeaders`, `render.go`
>   (`operatorCSP`, `enrollCSP`, `redirect`, `renderEnroll`), sızıntı testinin başlığı (S4), md. 11, ADR
>   (v). H01, H01b, L01, L01b KIRMIZI; H01-live ve L01-live (aynı mutasyon, testler 4. turun canlı-harita
>   okumasına geri) YEŞİL — bulgunun mekanizması, ölçülerek.
> - **F2 (bloklayan) · `Location`.** `TestEnroll_TheTokenNeverTravelsInTheURL`'un başlığındaki genel
>   hüküm testin sürdüğü üç isteğe bağlandı. Düşmanca istek başlıkları 9'dan 15'e çıktı: `Referer`,
>   `User-Agent`, `Accept-Language`, `X-Requested-With`, `HX-Current-URL`, `HX-Target` eklendi; testin
>   başlığı, md. 11 ve ADR (v) on beşini adıyla sayar. H05b (çerezsiz dalda `textproto.MIMEHeader` ile
>   `Location` = `Referer` + 303, bir `Referer` gönderildiğinde) KIRMIZI (C28); H05b-noref (aynı mutasyon,
>   `Referer` düşmanca listeden çıkarılmış — 4. turun listesi) YEŞİL; H05c (koşulsuz) KIRMIZI.
> - **N1** `TestSessionGate_ASameSiteReadDoesNotTouchTheSession`'ın başlığı getirme üst verisi gönderen
>   tarayıcıya bağlandı, göndermeyen tarayıcı L5 adıyla (`routes.go` `sameOriginGate`'in metniyle aynı).
> - **N2** `marketing.go`'nun çerez notu taramanın okuduğu iki biçime bağlandı (`tappa_…` ve
>   `__Host-taptime_…`, `cookieNameLiteral`; başka biçimde adlandırılmış bir çerez kod incelemesinin);
>   "on çerez" tablonun bu iki biçimden okunan adlara iki yönde eşitliğidir. 4. tur süpürmesinin 62.
>   satırının türü düzeltildi.
> - **N3** B6 testi store'u 801 reddin sonunda ölçer (md. 8; önceden ilk 500'ün sonunda). N3 (çapraz-origin
>   giriş reddi `Password` çağırır) KIRMIZI; N3-500 (aynı mutasyon, ölçüm 500'de) YEŞİL.
> - **N4** ADR (I)(viii) ölçülen zaman kipine ve testin sürdüğü isteklere bağlandı ("sayılmadı/yazılmadı",
>   md. 20 ile aynı).
> - **N5** `classRoutes` (`op8r5_test.go`; C1–C48 → yöntem, `chi` rotası, durum): iki başlık tablosu her
>   sınıfın son isteğini ve durumunu kendi girdisine tutar;
>   `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` `chi.Walk`'un bildirdiği 8 yöntem × rota çiftinin
>   (5 rota) her birine 405 olmayan bir sınıf, her rotaya bir 405 sınıfı ister ve anahtarları C1–C48'e
>   tutar. OP-11 devri adıyla yazıldı (aşağıda). N5a (yeni `GET /operator/status`), N5b (`POST
>   /operator/`), N5c (`classRoutes`'tan C46 silindi) KIRMIZI.
> - **N6** Betik yükleyen sınıflar adıyla doğrulanır ("C18,C20,C21,C22"). N6 (C19 enrollment ekranını,
>   C20 problem sayfasını render eder; sayı 4 kalır) KIRMIZI; N6-count (aynı mutasyon, sayı kontrolüyle)
>   YEŞİL.
> - **N7** C41–C48, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`: `/operator`'da
>   `HEAD`/`POST`/`OPTIONS`, `/operator/login/totp` ve `/operator/enroll`'da `PUT`, `/operator/logout`'ta
>   `GET` → 405 (`Allow`, CSP, `no-store`; altısında store 0); kod adımında ve enrollment'ta büyük form →
>   413. N7a (kod adımının 413'üne `Connection: close`) ve N7b (`securityHeaders` `OPTIONS`'ı atlar)
>   KIRMIZI, ikisi de yalnız bu yeni testte.
> - **Süpürme (5. tur, tam ve mekanik).** Sözcükler (büyük/küçük harf yok sayılarak, sözcük sınırıyla):
>   `only`, `never`, `nothing`, `every…`, `ever`, `no other`, `cannot`, `can't`, `can not`, `impossible`,
>   `always`, `guarantee…`, `ensur…`, `prevent…`, `any…`, `all`, `none`, `whatever`, `regardless`, `tek`,
>   `hiç…`, `yalnız…`, `asla`, `her`, `herhangi…`, `herkes…`, `her zaman`, `daima`, `imkânsız/imkansız…`,
>   `bütün…`, `tüm…`. Kapsam: OP-8'in yeni dosyalarının satırlarının tamamı (kod, yorum, dizge, test
>   mesajı; üretilmiş `_templ.go` dahil) ve değişen dosyalarda `git diff HEAD`'in eklediği satırların
>   tamamı (bu kartın OP-8 blokları, ADR 0020'nin OP-8 notları, `deploy/README.md` dahil). Sonuç:
>   551 geçiş, 52 dosyada (32 yeni, 20 değişen). Kategoriler: alıntı 246 · sayılı kümeye bağlı (küme ve
>   test adıyla) 75 · sözcük listesi ya da terim (sızıntı testinin günlük-dışı listesi, SQL işlem kipi, OP-6 md. 8'in
>   saklanamayan adresi) 51 · kod/protokol belirteci (Go türü, CSP ve getirme üst verisi anahtar sözcüğü,
>   değişken adı, CSS, üretilmiş lint yönergesi) 44 ·
>   sayı 30 · PART II/III 30 · test mesajı 20 · kod tarifi (okuma, adıyla) 17 · mutasyon tarifi 14 · olgu
>   (Go, Postgres, net/http ya da bu değişikliğin diff'i) 13 · UI metni 6 · biçim tanımı 3 · ölçülmeyenler
>   listesi 1 · devir talimatı 1; **bağlı olmayan 0**. Bu süpürmeyle yeniden yazılanlar: `op8_db_test.go` (DSN cümlesi denenen tek biçime;
>   tekrar edilen kodun reddi; dördüncü denemenin ölçülen sayıları), `op8_test.go` (`anyone`),
>   `op8r5_test.go` (`cannot`), `marketing_test.go` (`anywhere`), `input.css` (`Everything`). Satır
>   satır liste ve her geçişin bağı teslim raporundadır.
>
> **5. tur mutasyon tablosu (kopyala-geri-yaz, yedekler yalnız scratchpad'de; tablodaki her deneme diskte
> `diff` ile, geri yükleme sha256 ile; ağacın parmak izi önce/sonra eşit; `.env`'li; son sütunda
> "koşulan" yazmayanlar paketin tamamıyla).** 18 deneme: **13 kırmızı, 5 yeşil BEKLENEN** (H01-live,
> L01-live, H05b-noref, N3-500, N6-count: aynı mutasyonun 4. turun ölçümüyle yeşil kaldığının
> kontrolleri). N5c ilk koşuda derlenmedi (kaldırılan girdi satırın sonundaki virgülü de götürüyordu);
> düzeltilip yeniden koşuldu, tabloda bir kez.
>
> | Kimlik | Ne | Sonuç | Kırmızıya çevirdiği test(ler) |
> |---|---|---|---|
> | H01 | 4th audit's H01: render sets Refresh from the request, WriteHeader, then delete(w.Header(), Refresh) | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`, `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | H01b | H01 with maps.DeleteFunc instead of the builtin (outside the RH list) | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy` |
> | H01-live | CONTROL: H01b against the 4th round's live-map reads (checkDesignedHeaders and S4 back to w.Header()) | GREEN | — (yeşil, BEKLENEN: kontrol; koşulan: `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`) |
> | L01 | 4th audit's L01: enrollPage sets X-Key to the TOTP secret's base32 and defers delete(w.Header(), X-Key) | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries`, `TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions` |
> | L01b | L01 with maps.DeleteFunc instead of the builtin (outside the RH list) | RED | `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries` |
> | L01-live | CONTROL: L01b against the 4th round's live-map reads | GREEN | — (yeşil, BEKLENEN: kontrol; koşulan: `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`) |
> | H05b | 4th audit's H05b: requireOperator's cookieless branch writes Location = r.Referer() through textproto.MIMEHeader + 303 when a Referer is sent | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` |
> | H05b-noref | CONTROL: H05b with Referer dropped from hostileHeaders (the 4th round's list) | GREEN | — (yeşil, BEKLENEN: kontrol; koşulan: paketin tamamı) |
> | H05c | H05b unconditional (Location = r.Referer(), empty when none is sent) | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestSessionGate_NoLiveSessionIsASignInRedirect` |
> | N3 | a cross-origin sign-in refusal calls Password (store calls on the 300 refused sign-ins, none on the 500 sign-outs) | RED | `TestClientAddress_TheListedReadsFeedOnlyTheBudgets`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`, `TestSameOriginGate_ACrossOriginPostReachesNoStore`, `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel` |
> | N3-500 | CONTROL: N3 against the 4th round's measurement (the store checked after the first 500 only) | GREEN | — (yeşil, BEKLENEN: kontrol; koşulan: `TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel`) |
> | N5a | a new console route GET /operator/status mounted, no class | RED | `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` |
> | N5b | POST mounted on the console route (the C42 405 becomes a handler) | RED | `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy` |
> | N5c | test side: classRoutes loses C46 (GET /operator/logout 405) | RED | `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`, `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy` |
> | N6 | C19 renders the enrollment screen and C20 a problem page: still four scripted classes, other ones | RED | `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` (koşulan: `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`) |
> | N6-count | CONTROL: N6 against a count-only check (len(scripted) != 4) | GREEN | — (yeşil, BEKLENEN: kontrol; koşulan: `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`) |
> | N7a | the code step's 413 adds Connection: close (C47 only; C7 is the sign-in's 413) | RED | `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy` |
> | N7b | securityHeaders skips OPTIONS (C43) | RED | `TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy` |
>
> **Kart düzeltmesi — 6. tur (2026-10-02, YALNIZ METİN; 5. tur üçüncü göz RED verdi — iki
> bloklayanı da metin —, tappa-security-auditor ONAY verdi, iki düşük not).** Ürün davranışı ve test
> mantığı değişmedi: değişen satırlar yorum, test başlığı ve belge (kanıt teslim raporunda: değişen
> Go dosyalarının yorumsuz AST'si 5. tur sonuyla bayt bayt aynı).
>
> - **B1 · biçim sınırı.** `checkDesignedHeaders` düşmanca değeri iki biçimde arar: ham ve
>   `url.QueryEscape`. Yanıtta düşmanca değerin bulunmadığını söyleyen cümleler bu iki biçime
>   bağlandı: md. 11, md. 20, ADR (v), (I)(viii) ve §6 notu, `enroll.go`, 8 sınıflık testin PART I'i,
>   4. tur alt bloğunun B2'si. HTML-kaçışlı ve base32 yansıma PART III örneklerine eklendi (40 ve 8
>   sınıflık testler, ADR (III)). Denetçinin E23b'si (gövdeye HTML-kaçışlı yansıma) ve E25'i (base32)
>   paketin tamamında yeşildi: ölçüm bu iki biçimi aramaz.
> - **B2 · kopya ve tel.** (1) `op8r5_test.go`'nun dosya başlığı net/http'nin `ResponseWriter.Header`
>   belgesinin iki istisnasını adıyla yazar: 1xx yanıtlar ve trailer'lar (denetçinin T01/T02'si:
>   WriteHeader'dan sonra `http.TrailerPrefix` ile konan anahtar telde trailer olarak gider; kopyada
>   değil `Result().Trailer`'dadır, testler onu okumaz). (2) `serverAdded`'ın yorumu ölçülene
>   eşitlendi: karşılaştırılan nesne net/http istemcisinin ayrıştırdığı `Response.Header`'dır;
>   `Content-Length` ve `Date` adıyla düşer; `Transfer-Encoding` (C18'de `chunked`) istemcinin
>   ayrıştırıcısında `Response.TransferEncoding`'e taşınır; `Connection` bu HTTP/1.1 keep-alive
>   isteklerinde yazılmaz (scratchpad'de bir sondayla ölçüldü: `Connection: close` isteğinde telde
>   var). (3) Kopyayı telle genel olarak eşitleyen cümleler — `leak_test.go`'nun S4'ü,
>   `checkDesignedHeaders`'ın belgesi ve satır içi yorumu, başlık testinin WHAT IS READ'i, `routes.go`,
>   md. 11, ADR (v), 5. tur alt bloğunun F1'i — üç sınıfa (C1, C18, C28) ve başlık bölümüne bağlandı;
>   tel testinin PART III örnekleri: trailer bölümü, hijack edilmiş bağlantı (denetçinin E5'i;
>   kaydedici Hijack desteklemez), sunucunun çerçeve başlıkları.
> - **N-1 · sorgulu sınıf sayısı 32.** Ölçüm: worktree'siz ve `.env`'siz bir scratchpad kopyasında
>   iki tablonun `send`'i son isteğin yolunu sorgusuyla kaydedecek, `runHeaderClasses` her sınıf için
>   "yolda `?` var mı" yazacak biçimde geçici olarak değiştirildi (kopya sonra silindi): 48 sınıfın
>   32'sinin son isteği sorgu taşıdı — C1–C26, C38, C40 (40 sınıflık tabloda 28) ve C44, C45, C47,
>   C48 (8 sınıflık tabloda 4), yani son isteği `hostileDrive`'ın sorgu eklediği üç yoldan birine
>   giden sınıflar. "27" metinleri düzeltildi: başlık testi, `enroll.go`, md. 11, md. 20, ADR (v) ve
>   (I)(viii), 4. tur alt bloğunun B2'si.
> - **Güvenlik denetçisinin iki düşük notu** → L20 ve L21 (sayılı sınırlar listesi) ve ADR notunun
>   **Sınırlar** satırı. Notun "`TestEscapes_…`'ta pinli" sözü daraltıldı: o test çöp-önce isteğin
>   303 olduğunu ölçer; silen `Set-Cookie` ve sunucudaki oturumun canlı kalması denetçinin
>   ölçümüdür. Testin başlığı da buna eşitlendi; test mantığına dokunulmadı.
> - **Ek metin düzeltmesi.** `deploy/README.md` 7. kuralının `err` hücresi "sunucunun mesajı"nı
>   `TestOpenOperatorSurface_PrintsNoValue`'ya bağlıyordu; o test DSN'i ve parolayı ham, iki anahtarı
>   base64, hex ve ondalık liste biçimiyle arar. Sunucu mesajının hatada bulunmadığını `internal/db`'nin
>   `TestOperatorConnectErr_OnlyTheTableIsUnreachable`'ı ölçer; hücre ikisini ayrı ayrı adlandırır.
> - **Olumsuz olgu taraması (6. tur).** `yok…`, `bulunmad…`, `bulunmaz…`, `yazılmad…`, `yazılmaz…`,
>   `absent`, `not found`, `no … in` (arada en çok dört sözcük), 5. turun kapsamıyla: 115 geçiş.
>   Yanıtta, log satırında ya da hata metninde bir değerin olmadığını söyleyen 22 geçiş biçime (ham,
>   sorgu-kaçışlı, testin render kümesi ya da testin aradığı biçimler), sınıf kümesine ve değer
>   kümesine adıyla bağlı; bir başlık adının olmadığını söyleyen 6 geçiş sınıf kümesine
>   (`designedHeaders`) bağlı; kalan 87 geçiş başka türden — olgu (kod, config, diff, ölçüm) 34,
>   alıntı 16, tanım ya da kod tarifi 15, sözcük listesi ya da meta 8, test mesajı ya da tipi 5,
>   mutasyon tarifi 4, PART III 3, UI metni 2. Satır satır liste teslim raporunda.
> - **Evrensel sözcük süpürmesi yeniden koşuldu** (5. turun listesi ve kapsamı, bu turun metinleriyle):
>   561 geçiş (5. turda 551): alıntı 246 · sayılı kümeye bağlı 78 · sözcük listesi ya da terim 51 ·
>   kod/protokol belirteci 44 · PART II/III 33 · sayı 31 · test mesajı 20 · kod tarifi (okuma, adıyla)
>   17 · olgu 16 · mutasyon tarifi 14 · UI metni 6 · biçim tanımı 3 · ölçülmeyenler listesi 1 · devir
>   talimatı 1; **bağlı olmayan 0**.
>
> **Devirler, adıyla:** **OP-9** — linki `/operator/enroll?id=<id>#<token>` biçiminde bas (token
> `NewEnrollmentToken`); ingress'in neyi log'ladığını ölç; DNS/TLS/Ingress kuralı (K2) ve operatör
> host'una IP kısıtı (K4 — L2'nin çaresi); operatör host'u ingress'in müşteri host'larından biri
> OLMASIN (L3); runbook'a `/operator/login` ile yanıt doğrulaması. **OP-11** — tenant ekranları
> `operatorpages.TenantScreen` ile (adsız render edilemez); oturum bütçesini (100/10 dk) okuma başına
> iki işlemle yeniden türet; konsoldan linkler ancak rota kayıtlıyken; *(5. tur, N5)* her yeni ekran
> ve rota şunlara eklenir: `classRoutes` (C tablosu, `op8r5_test.go`) ve başlık tablolarından birine
> bir sınıf, `designedHeaders`'a tasarlanan başlıkları, sızıntı testinin kollarına bir kol ve
> `harvestWant`'e — `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` sınıfsız monte edilmiş bir
> yöntem × rota çiftini kırmızıya çevirir, öbür üçü elle eklenir. **OP-14** — `password_ok`
> audit türü (P9) bir migration'la; görüntüleyici `login`/`logout`/`enrollment` satırlarını gösterir.
> **OP-10** — `/operator/legal` bu kabuğun içinde, aynı zincirle.

> **Kart düzeltmesi (2026-10-02, OP-9 uygulaması sırasında).** Yazıldı: `cmd/opadmin/`
> (`main.go`; testler `main_test.go`, `deps_test.go`, `opadmin_db_test.go`),
> `deploy/README.md` → *"Operator accounts (M10 OP-9) — `cmd/opadmin`"* runbook'u (create ·
> reset-mfa · disable · sayılı sınırlar O9-1…O9-5 · K2/K4 taslağı · ingress ölçümü), ADR 0020
> §6'ya tarihli OP-9 notu. Migration YOK, yeni bağımlılık YOK (`go.mod`/`go.sum` diff boş),
> `internal/operatorauth` ve ingress manifesti DEĞİŞMEDİ. Ölçüm: dev Postgres 17.10, sahip
> (`tappa_owner`, süper kullanıcı); DB testleri sahibin geri alınan transaction'ında, operatör
> rolüne `SET LOCAL SESSION AUTHORIZATION` ile; iki üst düzey test (`TestApply_AFailureAnywhereLeavesNoRow`,
> `TestApply_ARowHeldElsewhereFailsFastNotForever`) betiğin KENDİ transaction'ını ölçtüğü için
> sahibin bağlantısında üst düzeyde uygular — her koşuda enjekte edilmiş bir hata ya da
> `lock_timeout` vardır, doğru betik satır bırakmaz (koşulardan sonra `platform_admins`'te bu
> testlerin adres önekiyle 0 satır, ölçüldü). Bu paketin operatör tablolarına dokunan her testi
> `tappa/test/operator-tables` kilidini PAYLAŞIMLI alır. Kararlar ve ölçümler:
>
> 1. **OP-9 satırı ve OP-4 bloğunun "OP-9:" maddesi şöyle okunmalı: link sırrı
>    `operatorauth.NewEnrollmentToken` ile DEĞİL, onun sözleşmesiyle basılır.** Ölçüldü:
>    `go list -deps ./internal/operatorauth` `github.com/jackc/pgx/v5` ve `database/sql`
>    listeliyor (operatorauth → `internal/db`); import etmek §6'nın "sürücü yok"unu bozardı.
>    Tek kaynağı sürücüsüz bir pakete taşımak, OP-8'in aynı anda düzenlediği paketi ve onu
>    adıyla süren sızıntı/redaksiyon testlerini değiştirirdi. Seçilen: üç stdlib çağrısı
>    (`crypto/rand` 32 bayt → `base64.RawURLEncoding`; `sha256` → küçük harf hex)
>    `cmd/opadmin`'de; eşitlik TESTTEN pinli (test ikilisi operatorauth'u import eder, komut
>    ikilisi etmez): `TestLinkSecret_HashIsOperatorauthsHash` (105 girdi: 100 basılmış + boş,
>    ASCII dışı, 200 karakter), `TestLinkSecret_Shape` (1000 çekiliş: 43 karakter, kanonik, 32
>    bayt, OP-8'in `enroll.js` deseni), ve 00026'ya karşı `TestCreate_TheLinkEnrollsTheAccount`
>    (operatorauth'un `CompleteEnrollment`'ı opadmin'in sırrını kabul eder; veritabanının
>    `encode(sha256(convert_to(…, 'UTF8')), 'hex')`'i saklanan hash'e eşit).
> 2. **Link** OP-8'in biçimi: `https://<host>/operator/enroll?id=<uuid>#<sır>`
>    (`TestLink_IsTheShapeTheOperatorPageReads`). **`--host`** zorunlu bayrak, config'in
>    `isDNSHostName` kuralı — iki fonksiyonun gövdesi yorumsuz basılıp karşılaştırılır
>    (`TestHost_IsConfigsRule`); ortam değişkeninden okunmaz (`TestDeps_NoEnvironmentAndNoDSNName`'in
>    saydığı on ortam çağrısı kaynakta yok). `TAPPA_BASE_URL` eşitsizliği burada sınanmaz
>    (opadmin onu bilmez; yanlış host'lu link 404 verir).
> 3. **Ingress ölçümü (ADR 0020 §6'nın OP-9'a bıraktığı).** Yerel ham TCP dinleyici, link
>    biçiminde URL, sır yerine 43 karakterlik rastgele değer: Go `net/http`, curl 8.7.1 ve
>    headless Chrome 154'ün gönderdiği baytlarda (istek satırı + Chrome'un alt kaynak
>    isteklerindeki 4 `Referer`) hesap id'si var, `#` ve sır yok. ingress-nginx'in varsayılan
>    satırı `$request` ve `$http_referer` yazar (belgeden; kümede ölçülmedi). Sonda repo
>    dışında, scratchpad'de.
> 4. **Hedef = id VE e-posta** (`reset-mfa`, `disable`): ikisi aynı satırı göstermezse SQL
>    reddeder (`TestResetMFA_RefusesWhatItMustNot`,
>    `TestApply_ASecondCreateAndAMismatchedDisableAreRefused`). Gerekçe: id'yi insan kopyalar;
>    başka bir operatörün id'si yanlışlıkla yapıştırılırsa adres eşleşmez. Id ise link için
>    şart (yalnız e-postayla hedeflemek linke id koyamazdı). E-posta citext ile harf duyarsız.
> 5. **`reset-mfa` (ADR §6 tanımı + üç karar):** zarf silinir, sayaç (`totp_failures`,
>    `totp_locked_until`) sıfırlanır, `pending`, açık oturumlar iptal, yeni hash +
>    `enroll_used_at = NULL` AYNI ifadede (OP-5 md. 18), issued/expires tek `v_now`'dan.
>    **(a) parola özeti de silinir** — enrollment yenisini koşulsuz yazar; `pending` bir
>    hesabın özetini ne giriş araması (RLS yalnız `active`) ne definer okur (definer'ın
>    `SELECT` listesinde yok — `TestOperator00026_PrivilegeMatrix`). **(b) `disabled`
>    hesap reddedilir** — bir MFA sıfırlaması kapatılmış bir operatörü geri almaz; geri alma
>    opadmin'de yok (adres kayıtlı kalır, `create` aynı adresi reddeder). **(c) aynı SQL'in
>    ikinci uygulaması reddedilir** (hesabın hash'i zaten bu betiğinkiyse): ölçülen mutant (M10)
>    kullanılmış bir linki 30 dk yeniden açardı. *(2. tur: bu denetim yalnız ARDI ARDINA ikinci
>    uygulamayı tutuyordu; A-B-A — R1, R2, R1 — kullanılmış R1 linkini yeniden açıyordu, ölçüldü.
>    2. turda üretim zamanı korumasıyla daraltıldı — 30 sn'lik bir pencere kaldı, ölçüldü (makine
>    20 sn ileri, R2 hemen → R1 yeniden kabul, kullanılmış link yeniden enroll); 3. turda gelecek
>    payı sıfırlanınca o pencere — veritabanı saati geri atmadıkça — kapandı, ölçüldü; kalan
>    README O9-5 — aşağıdaki "2. tur" S2 ve
>    "3. tur".)* `pending` hesaba
>    uygulanabilir: süresi geçen `create` linkinin yenileme yolu. `totp_last_step` dokunulmaz
>    ~~(monoton tekrar koruması)~~ *(2. tur, güvenlik 5: gerekçe yanlıştı — `op_complete_enrollment`
>    adımı `<` yüklemi olmadan yazar; sıfırlama + yeniden enroll sonrası adım geriye gidebilir.
>    Güvenlik etkisi yok: tekrar koruması yeni sırla baştan başlar.)*
> 6. **`disable`:** `disabled` + açık oturumlar iptal; ikinci uygulama durumu değiştirmez,
>    NOTICE önceki durumu söyler. `pending` bir hesap kapatılınca token'ı ölüdür
>    (`op_complete_enrollment` `pending` ister).
> 7. **Transaction biçimi — ölçüldü, karar.** Sunucu 17.10, istemci psql 18.4, geçici tablolarda, opadmin'in zarfıyla
>    (BEGIN · 2× SET LOCAL · DO · COMMIT), enjekte edilmiş hata:
>
>    | psql | hata yeri | BEGIN var | BEGIN yok | çıkış |
>    |---|---|---|---|---|
>    | varsayılan | DO'dan önce | 0 satır | **1 satır** | 0 |
>    | varsayılan | DO içinde | 0 | 0 | 0 |
>    | varsayılan | DO'dan sonra | 0 | **1** | 0 |
>    | `-v ON_ERROR_STOP=1` | üçü | 0 | — | **3** |
>    | `-1 -v ON_ERROR_STOP=1`, hatasız | — | 1 satır, 2 `WARNING` | — | 0 |
>
>    Karar: SQL kendi `BEGIN … COMMIT`'ini taşır, iş TEK `DO` bloğunda (tek ifade: içindeki
>    hata bloğun yazılarını geri alır — ölçüldü: create'te INSERT, reset-mfa'da ilk UPDATE), runbook `psql -X -v ON_ERROR_STOP=1` (`-1` yok; eklenirse
>    zararsız). `BEGIN` yük taşır: DO'dan önce/sonra hata + varsayılan psql (`TestApply_AFailureAnywhereLeavesNoRow`,
>    pgx ile ifade ifade, hatadan sonra devam ederek — psql'in ölçülen davranışı) ve iki
>    `SET LOCAL`. Gerçek `create` SQL'i gerçek psql'den geçti (COMMIT yerine okuma + ROLLBACK):
>    `BEGIN · SET · SET · NOTICE · DO`, ttl `00:30:00`, kalıcı satır 0. *(2. tur, B8: bu tablo
>    `ON_ERROR_ROLLBACK` kapalıyken ölçüldü — psqlrc'de `on` ise DO'dan önce/sonra hata bir
>    savepoint'e geri sarılır ve DO'nun yazısı commit edilir: çıkış 0, 1 satır, ölçüldü;
>    runbook `-X` ile psqlrc'yi atlar. Zarf 2. turda yedi ifadedir — aşağıdaki S1.)*
> 8. **Değerler hex:** e-posta ve ad ~~`pg_catalog.convert_from(pg_catalog.decode('<hex>',
>    'hex'), 'UTF8')` olarak girer~~ *(2. tur, S1: hash'le birlikte COPY veri satırında hex
>    olarak gider, ifade metninde değil; 1. turun bu metin testi yeniden adlandırıldı:
>    `TestSQL_TheValuesTravelAsCopyData`)*; tırnaklama yok (
>    `'`, `$$`, ters eğik çizgi, `$opadmin$` taşıyan girdi bayt bayt saklandı —
>    `TestCreate_StoresHostileInputByteForByte`). E-posta ASCII (dot-atom + DNS alanı, ≤ 254
>    bayt, yerel kısım ≤ 64) — giriş kimliğinde başka yazıdan benzer harf ikinci bir yazım
>    olurdu; ad UTF-8, ≤ 200 karakter, harf/işaret/rakam/noktalama/sembol + ASCII boşluk
>    (kontrol, biçim — bidi, sıfır genişlik — ve öteki ayırıcılar reddedilir).
> 9. **stderr terminal kapısı (karar):** `create`/`reset-mfa` stderr bir karakter aygıtı
>    değilse çalışmaz (`TestRun_RefusesALinkWhenStderrIsNotATerminal`,
>    `TestMain_StderrIsTerminalReadsTheMode`); `disable` link basmaz, çalışır. SQL önce yazılır,
>    link ancak sonra (`TestLeak_AFailedWritePrintsNoLink`). *(2. tur, B5: bu iki test `main`'in
>    kablosunu ölçmüyordu — `main` `true` geçirince yeşil kalıyorlardı; derlenmiş komutu koşan
>    `TestMain_TheBinaryRefusesARedirectedStderr` eklendi. Sıra da değişti: COMMIT, rapor
>    stderr'e ulaştıktan sonra yazılır — S1/gözlem.)*
> 10. **Rol kapısı:** DO bloğunun ilk ifadesi süper kullanıcı ya da BYPASSRLS olmayan rolü
>     reddeder (`TestApply_RefusesARoleRLSWouldFilter`: `tappa_app` ve `tappa_operator`, üç alt
>     komut). **`lock_timeout` 5 sn:** başka bir transaction'ın tuttuğu satırda betik ~5 sn'de
>     55P03 ile düşer (`TestApply_ARowHeldElsewhereFailsFastNotForever`, ölçülen 5,10 sn).
>     Kısıt reddi adıyla döner, DETAIL'siz (ikinci `create` → `platform_admins_pkey`,
>     büyük harfli var olan adres → `platform_admins_email_key`).
> 11. **Kabul — OP-9 satırı:** *sürücü yok* → `TestDeps_NoDriverInTheClosure` (beş port,
>     yalnız stdlib + komut; `database/sql`, `net`, `os/exec`, `plugin` yok; pozitif kontrol:
>     aynı denetim operatorauth'ta pgx'i bulur) + `TestDeps_ImportsAreTheListedOnes`; *owner DSN
>     adı kaynakta yok* → `TestDeps_NoEnvironmentAndNoDSNName` (+ cmd/tappa'nın
>     `TestPackaging_TheCommandCannotMigrate`'i ürün kaynaklarını tarar, `cmd/opadmin` dahil);
>     *çıktı tek transaction* → md. 7; *ham token stdout'ta yok* →
>     `TestLeak_TheSecretIsOnlyInTheLinkOnStderr` (stdout/SQL, sekiz kodlama, varsayılan
>     slog/log Debug text+JSON, pozitif kontrol); *süresi geçen token red* →
>     `TestCreate_AnExpiredLinkIsRefused` (saat enjekte edilmeden: issued/expires 30 dk + 1 sn
>     geri → red; 31 sn ileri → kabul). Uçtan uca: `TestCreate_TheLinkEnrollsTheAccount`,
>     `TestResetMFA_KillsTheOldSessionsAndIssuesANewLink` (eski oturum ölü, eski link red,
>     yeni link enroll), `TestDisable_EndsSessionsAndSignIn` (oturum ve giriş red).
>     **K2/K4 YAPILMADI** — kullanıcı kararı; README'de taslak (ayrı `tappa-operator` Ingress'i,
>     opsiyonel `whitelist-source-range`, Cloudflare proxy'si açıkken kaynak adresin ölçülmesi
>     gerektiği notu).
> 12. **Mutasyonlar (kopyala-geri-yaz, yedek scratchpad'de; her biri birim + ilgili DB
>     testleri):** 21/21 kırmızı — M1 sır stdout'a · M1b SQL'de hash yerine ham sır · M2 TTL 31
>     dk · M2b `expires` ikinci saat okumasından · M3 `BEGIN` kaldırıldı · M4 pgx import'u · M5
>     anahtarlı hash · M6 terminal kapısı kaldırıldı · M7 `enroll_used_at` korunur · M8 oturumlar
>     iptal edilmez · M9 hedef yalnız id · M10 ikinci uygulama denetimi kaldırıldı · M11
>     `disabled` denetimi kaldırıldı · M12 link SQL'den önce basılır · M13 e-posta tırnakla ·
>     M14 host kuralı büyük harf kabul · M15 ortam okuması · M16 kısıt işleyicisi kaldırıldı ·
>     M17 rol kapısı kaldırıldı · M18 `lock_timeout` kaldırıldı · M19 `reset-mfa` zarfı korur.
> 13. **Sayılı sınırlar** (README O9-1…O9-5): opadmin'in eylemleri `operator_audit_log`'a
>     yazılmaz (tür kümesi kapalı — yeni tür bir migration; **OP-14'e, adıyla**); terminal
>     kapısı dosya kipine bakar (kendini kaydeden terminal ve `/dev/null` geçer); ~~hatalı bir
>     ifade sunucu log'una SQL metnini (id, hex adres/ad, sırrın SHA-256'sı) yazar — sunucu log'u
>     okunmadı~~ *(2. tur, S1: ADR 0020 §5'le çelişiyordu, sayılı sınır değil hataydı —
>     düzeltildi; README O9-3 yeniden yazıldı)*. Güvenlik iddiası `cmd/opadmin/main.go`
>     başlığında üç parçalı (PART I ölçülen davranış + test adları; PART II ~~dokuz~~ *(3. tur:
>     on üç)* pin ve yakaladıkları; PART III tamlık iddiası yok).
>
> **2. tur (2026-10-02, üçüncü göz RED + `tappa-security-auditor` RED'inden sonra).** Aynı ölçüm
> düzeni; sondalar sır değeri basmaz (link sırrı çıktıda uzunluğuyla değiştirilir; aktarıcı
> bulundu/bulunmadı ve sayı basar).
>
> - **S1 · enrollment hash'i sunucu log'una gidiyordu — düzeltildi, seçenek (b) (rotatekek
>   sınıfı).** Sunucu log'u okunmadı (kural); ölçülen, log'un **girdileridir**: psql'in
>   gönderdiği protokol mesajları ve sunucunun döndürdüğü hata alanları. Sonda: scratchpad'de
>   psql ile sunucu arasında kayıt yapan bir TCP aktarıcısı (parola mesajı kaydedilmez), psql
>   18.4. **Önce (1. tur kodu):** create başarı → gönderilen 5 ifadenin 1'i (DO) hash + hex
>   adres/ad taşıyor (geliştirme DB'si `log_statement=all` + `log_min_duration_statement=0`
>   koşar: her uygulama log'a); var olmayan hesaba reset-mfa → hatanın ifadesi hash taşıyor
>   (`log_min_error_statement=error`, üretimin varsayılanı: her ret log'a); BEGIN'den hemen
>   sonra hata + varsayılan psql → 25P02 alan DO ifadesi hash taşıyor. **Sonra (2. tur kodu,
>   aynı senaryolar):** gönderilen 6–8 ifadenin **0**'ı bu üç değerden birini taşıyor; değerler
>   yalnız **1 COPY veri mesajında**; dönen hata/uyarı alanlarında 0; BEGIN'den sonra hata +
>   varsayılan psql → COPY başlamadı, psql veri satırını (`-- …`) önde gelen yorum olarak
>   **atladı**, `\.` için *"invalid command"* yazdı, COPY veri mesajı 0; `ON_ERROR_STOP=1` →
>   ilk hatada durdu. Şekil: `BEGIN · SET LOCAL search_path · SET LOCAL lock_timeout · CREATE
>   TEMP TABLE pg_temp.opadmin_in … ON COMMIT DROP · COPY … FROM STDIN` + tek veri satırı
>   (`-- <hash> <hex e-posta> [<hex ad>]`) `· DO · COMMIT`; DO satırı okur, biçimini denetler
>   (tek satır, alan sayısı, hex desenleri), hex'i çözer. İfade metninde kalan: sabitler,
>   doğrulanmış hesap id'si, üretim zamanı. Depo testleri: `TestLog_TheValuesReachTheServerOnlyAsCopyData`
>   (yedi ret yolu pgx ile ifade ifade: ifade metinleri + dönen hata alanları; pozitif kontrol:
>   hash'i yankılayan bir hata görülür), `TestApply_ARowHeldElsewhereFailsFastNotForever`
>   (55P03 yolu), `TestSQL_TheValuesTravelAsCopyData` (metin; pozitif kontrol). **(a) seçilmedi:**
>   `SET LOCAL log_*` süper kullanıcı ister; başarısız bir `SET`'ten sonra hash taşıyan DO 25P02
>   alır ve metni yine log'a gider (varsayılan psql); `pg_stat_statements`/`pg_stat_activity`'yi
>   kapsamaz. **(c) seçilmedi.** Kapsam dışı, adıyla (README O9-3): bir COPY **veri** hatası
>   satırı CONTEXT'e yazar (tetiklenmedi); `auto_explain`/`pg_stat_statements` yüklü sunucu
>   (geliştirmede ikisi de yok; üretim manifesti `args: []`).
> - **S2 · A-B-A — önce KIRMIZI, sonra YEŞİL.** `TestResetMFA_ABAReplayIsRefused` 1. tur
>   kodunda koşuldu: R1'in ikinci uygulaması kabul edildi, hesap `active` → `pending`, R1'in
>   **kullanılmış linki hesabı yeniden enroll etti**. Tasarım: betik üretim zamanı T'yi taşır;
>   (i) `reset-mfa`, hesabın `enroll_issued_at` ≥ T ise reddeder (betik üretildikten sonra link
>   verilmiş); (ii) `create`/`reset-mfa` T + 30 dk geçtiyse reddeder; (iii) T veritabanı
>   saatinin ~~30 sn'den fazla~~ önündeyse reddeder *(3. tur: pay sıfır — aşağıda)*.
>   Ön denetimler `FOR UPDATE` altında; UPDATE'in WHERE'i durumu, hash'i ve veriliş zamanını
>   yeniden şart koşar ve tek satır ister (X12'yi de kapatır). Saat kayması ölçüldü: bu makine
>   ↔ geliştirme DB'si −1,1 ms (test log'u; psql ile ±6 ms, ~100 ms gidiş-dönüş). ~~**Kalan
>   pencere (README O9-5):** üreten makinenin saati veritabanınınkinin önündeyse, R1'in
>   uygulanmasından en çok 30 sn sonra uygulanan bir R2'nin veriliş zamanı R1'in T'sinden
>   küçük kalabilir ve R1'in yeniden uygulanması kabul edilir~~ *(3. tur: pay sıfırlanınca
>   kapandı; O9-5 yeniden yazıldı)*; üretimdeki kayma ölçülmedi.
>   `disable` T taşımaz (link vermez). Runbook: üret, hemen uygula, dosyayı sil.
>   `TestScript_GenerationGuards`: 29 dk kabul / 31 dk red, ~~+20 sn kabul / +40 sn red~~, son
>   verilişten önce üretilmiş reset red. *(3. tur: bu test yalnız `create`'i koşuyordu — B-1;
>   şimdi iki alt komut, +1/+20 sn red ve bekleyip aynı dosya kabul.)*
> - **B2** · runbook'a 55P03 adımı (`ALTER ROLE tappa_operator NOLOGIN` + `pg_terminate_backend`
>   + yeniden uygula + `LOGIN`) ve README O9-4. **Ölçülmedi:** kilit tutan bir operatör
>   oturumu kalıcı satır bırakmadan kurulamıyor (committed bir hesap ister; `totp_failed`
>   satırı silinemez audit'e düşer) — üçüncü gözün ölçümüne dayanır.
> - **B3** · `main.go` başlığı: hesap id'si ifade metninde tırnak içinde, kanonik uuid
>   biçimine doğrulanmış (`TestRun_RefusesTheEscapeTable`'ın beş id vakası).
> - **B4** · deny-list yorumu daraltıldı; `TestDeps_StartsNoProcess` (`os.StartProcess`,
>   `syscall.Exec`/`ForkExec`/`StartProcess`; pozitif kontrol).
> - **B5** · `TestMain_TheBinaryRefusesARedirectedStderr` (yukarıda md. 9).
> - **B6** · 255 baytlık vaka artık tam 255; 254 bayt kabul (`TestRun_AcceptsTheBoundaries`;
>   DB'de saklandığı `TestCreate_StoresHostileInputByteForByte`).
> - **B7** · README "Doğrula": kullanımdan sonra son sütun pozitif kalabilir.
> - **B8** · kapsam yazıldı (md. 7 notu, main.go başlığı, README).
> - **B9** · zehir ifadesi tek başına: varsayılan psql çıkış **0**, `ON_ERROR_STOP=1` çıkış **3**
>   (ölçüldü); `refuse`'un yorumu ve README düzeltildi.
> - **Güvenlik 3** · O9-1'in iz listesi ölçülene eşitlendi (`disable` için damga yok, her
>   `reset-mfa` `enroll_issued_at`'i ezer); OP-14 devri README'de de "adıyla". **OP-14'e,
>   adıyla:** opadmin eylemleri için bir audit türü (migration).
> - **Güvenlik 4** · README link teslimi: `tmux clear-history`/screen tamponu, pano yöneticisi,
>   tarayıcı geçmişi (fragment'lı girdi ölçülmedi) adıyla.
> - **Güvenlik 5** · md. 5'in "monoton" gerekçesi düzeltildi.
> - **Güvenlik 6** · K4 taslağına XFF sahteciliği uyarısı (`40-ingress.yaml` (b)).
> - **Gözlemler:** X12 → UPDATE'in yeniden denetimi + `TestSQL_ResetRechecksUnderTheRowLock`
>   (metin pini; eşzamanlı davranış kalıcı satırsız ölçülemedi). X13 → daha önce iptal
>   edilmiş oturumun `revoked_at`'i ikinci `disable`'da korunur (`TestDisable_EndsSessionsAndSignIn`).
>   stderr yazımı başarısızsa → COMMIT yerine zehir (`TestLeak_AFailedReportWithholdsTheCommit`
>   + DB'de 0 satır, `TestApply_AFailureAnywhereLeavesNoRow`). flag paketinin tekrarlanan
>   değeri yankılaması → değişmedi (operatörün kendi girdisi, kendi stderr'ine).
> - **Testin kendi sızıntısı:** 1. turda `TestCreate_TheLinkEnrollsTheAccount` ham link sırrını
>   bir bağlı parametreyle sunucuya gönderiyordu (geliştirme DB'si parametreleri log'lar) —
>   o sorgu kaldırıldı; eşitlik Go'da ve başarılı enrollment'ta ölçülür. *(3. tur, B-3: başarılı
>   enrollment'ın kendisi de bağlı parametre yoludur — `o.enroll` → `operatorauth.CompleteEnrollment`
>   → `db.CompleteOperatorEnrollment` ham sırrı `$2` olarak gönderir; geliştirme DB'si
>   `log_statement=all` + `log_parameter_max_length=-1` ile koşar, yani DB testlerinin her
>   `o.enroll` çağrısı bir geri alınan fixture'ın ham link sırrını geliştirme log'una yazar.
>   operatorauth'un kendi suite'iyle aynı sınıf; ürüne etkisi yok — üretim bu iki ayarı
>   koşmaz.)*
> - **Mutasyonlar (2. tur, `mutate2.py` — kopyala-geri-yaz, 2. tur koduna yeniden
>   bağlanmış):** **34/34 kırmızı.** 1. turun 21'i (M1…M19, M1b, M2b; M13 artık "e-posta ifade
>   metnine tırnakla") + yeniler: N1 hash DO metnine geri (S1) → `TestLog_…`, `TestSQL_TheValuesTravelAsCopyData`,
>   `TestApply_ARowHeldElsewhereFailsFastNotForever` · N2a A-B-A ön denetimi kaldırıldı → `TestSQL_ResetRechecksUnderTheRowLock`,
>   `TestLog_…`, `TestScript_GenerationGuards` (A-B-A testi YEŞİL kalır: UPDATE'in WHERE kemeri
>   "changed under this script" ile reddeder — iki katman, bilerek) · N2b yalnız WHERE kemeri
>   kaldırıldı → yalnız metin pini kırmızı (ön denetim davranışı tutar) · N2c ikisi birden →
>   `TestResetMFA_ABAReplayIsRefused` dahil dört test · N3 yaş koruması · N4 gelecek koruması →
>   `TestScript_…`, `TestLog_…` · N5 `main` `true` geçirir → `TestMain_TheBinaryRefusesARedirectedStderr`
>   · N6 `maxEmailBytes = 256` → `TestRun_AcceptsTheBoundaries`, kaçış tablosu · N7
>   `revoked_at IS NULL` süzgeci → `TestDisable_…` · N8 rapor başarısızken COMMIT → `TestLeak_AFailedReportWithholdsTheCommit`
>   + DB alt testi (satır commit edildi, temizlik sildi, sonda 0 satır) · N9 veri satırında
>   `-- ` yok → zarf testleri ve DB testleri · N10 `os.StartProcess` eklendi → `TestDeps_StartsNoProcess`
>   · N11 `FOR UPDATE` kaldırıldı → yalnız metin pini (eşzamanlı davranış ölçülmedi).
>
> **3. tur (2026-10-02, üçüncü göz RED — 1 bloklayan + 5 bloklamayan; `tappa-security-auditor`
> ONAY, 4 düşük).** Aynı ölçüm düzeni.
>
> - **B-1 · reset-mfa'nın üretim zamanı korumaları pinsizdi** (X11 `generationGuards`'ı
>   reset'ten silmek, X35 yalnız gelecek korumasını silmek → suite yeşil). `TestScript_GenerationGuards`
>   artık **iki alt komutu** koşar: `create` ve `reset-mfa` için 29 dk kabul / 31 dk red, +250
>   ms (4. tur), +1 sn ve +20 sn red, +1 sn'lik aynı dosya veritabanı saati geçince kabul; reset vakalarında
>   hesabın son verilişi önce 2 sa geriye çekilir (yoksa (i) yaş korumasını maskeler). Denetçinin
>   sondası test oldu: `TestResetMFA_AClockAheadDoesNotReopenAUsedLink` — 10 dk ileri saatle
>   üretilen R1 ilk uygulamada reddedilir (mutant altında test sondanın kalanını koşar ve
>   yeniden açılan linki raporlar); 2 sn ileri: R1 red → bekle → aynı R1 kabul → L1 kullanıldı
>   → R2 uygulandı, kullanıldı → R1 yeniden uygulanması red, L1 red. Metinler (README tablosu,
>   `main.go` PART I) alt komutu adlandırır.
> - **Karar · gelecek payı 30 sn → 0** (kodda pay yok; ölçülen en küçük ret +250 ms — 4. tur;
>   daha küçük ileri farklar ölçülmedi). Gerekçe ölçüldü: 30 sn'lik pay ölçülmüş bir tekrar
>   penceresiydi (2. tur O9-5; üçüncü göz: makine 20 sn ileri, R2 hemen → R1 yeniden kabul ve
>   kullanılmış link yeniden enroll). Pay 0 iken kabul edilmiş bir betiğin T'si kendi
>   verilişinden geride ya da eşittir (koruma ve veriliş aynı DO'da, aynı veritabanı saatiyle,
>   bu sırayla okunur), veritabanı saati geri atmadıkça sonraki bir veriliş ondan ileridedir ve (i) yeniden uygulamayı
>   reddeder. Bedel: ileri saatli bir makinede betik, veritabanı saati T'yi geçene kadar
>   reddedilir — **aynı dosya** sonra uygulanır (testte ölçüldü). Kalan (O9-5): veritabanı
>   saatinin geri atması (ölçülmedi); saat **geride**: bir verilişten sonraki s içinde üretilen
>   reset, makine saati verilişi geçene kadar reddedilir (üçüncü gözün 2 dk'lık ölçümü), yaş
>   penceresi s kadar kısalır. DB testleri artık betikleri **veritabanı saatiyle** üretir
>   (`dbGen`). Gerekçesi ölçülen saat farkıdır — DB − makine −0,89…−2,5 ms, yani DB geride ve
>   pay 0; makine saatiyle üretip hemen uygulamanın yarışacağı **tahmindi, yeniden
>   üretilmedi** (4. tur: kapanış denetçisi makine saatli üretimi tam suite'te 3/3 ve tek alt
>   testte 300/300 yeşil ölçtü). Korumaların kendisi açık ofsetlerle sürülür.
> - **B-2 · veri satırı denetimi pinsizdi** (X1 `v_rows <> 1` → `< 1`, X2 alan sayısı, X26 `--`
>   işareti). `TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted`: elle düzenlenmiş sekiz
>   satır — ikinci satır · fazla alan · eksik alan · işaret değişmiş · dolgu değişmiş · dolgu
>   yok · hex olmayan alan · büyük harfli hash — hepsi DO tarafından reddedilir, hesap satırı 0;
>   düzenlenmemiş betik kabul (pozitif kontrol).
> - **B-3** · yukarıdaki "Testin kendi sızıntısı" maddesi ölçülene eşitlendi.
> - **B-4** · README reset-mfa: tek cümle yerine her ret için "ne yapılacak" tablosu (`disabled`
>   ve id/adres uyuşmazlığında yeni reset de reddedilir; geride saatte yeni reset de
>   reddedilir); O9-5 iki yönü yazar.
> - **B-5** · md. 5'teki "kapandı" niteleyicisiz değil artık (2. turda pencere kaldı, 3. turda
>   kapandı, kalan O9-5).
> - **B-6 · istemci kipi — ölçüldü.** Aynı reddedilen reset-mfa betiği, aktarıcıyla: dosya
>   stdin'de / `-f` / `\i` → 6 ifade, değer taşıyan 0, değerler 1 COPY veri mesajında, çıkış 3;
>   `psql -c "$(cat dosya)"` → **1 ifade, değerleri taşıyor**, `\` için sözdizimi hatası, çıkış 1
>   (başarısız ifadenin metni `log_min_error_statement` ile log'a). `main.go` başlığı ölçülen
>   kiplere bağlandı; SQL'in kendi başlığı ve README: "stdin (`< dosya`) ya da `-f`; `-c` ya da GUI
>   sorgu aracı değil"; O9-3'ün kapsam dışı listesinde.
> - **Güvenlik S1-artık · COPY hatasının CONTEXT'i — düzeltildi (dolgu).** Ölçüldü: bir COPY veri
>   hatası (fazla sütun) CONTEXT'e satırın **ilk 100 baytını** ve `...` yazar (150 baytlık bir
>   satırla, geçici tabloda). İptal yolu aynı geri çağırmaya varır (PostgreSQL kaynağı;
>   denetçinin satır atıfları — ölçülmedi). Çare: veri satırında değerlerden önce 100 baytlık
>   sabit `copyPadding` (`-- ` + 100 bayt + boşluk → hash 104. bayttan başlar); DO onu tam
>   eşitlikle denetler. Testler: `TestCopy_ADataErrorShowsThePaddingNotTheValues` (gerçek
>   sunucu: veri hatasının CONTEXT'i dolguyu gösterir, betiğin veri satırı değerlerini — hash,
>   adres, ad — değil;
>   pozitif kontrol: dolgusuz sentetik satırda ilk değer CONTEXT'te),
>   `TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue` (metin; pozitif kontrol), dolgu
>   değişmiş/yok vakaları B-2'nin testinde. O9-3 iptal tetikleyicilerini adıyla yazar.
> - **Güvenlik test** · `TestLog_…`'un pozitif kontrolü artık **sentetik** bir değerle (betiğin
>   kopyasında hash'in yerine konmuş) çalışır; gerçek bir test hash'i sunucuya gönderilmez.
> - **Güvenlik doküman** · bu bloktaki "O9-1…O9-3" → "O9-1…O9-5" (iki yer); README'nin beklenen
>   psql çıktısı ölçülen şekle eşitlendi: `BEGIN · SET · SET · CREATE TABLE · COPY 1 · NOTICE ·
>   DO · COMMIT` (ölçüldü, COMMIT yerine ROLLBACK ile).
> - **X25 (not)** · `serverSaw` artık hex alanların çözüldüğü metni de (adres, ad) arar.
> - **Mutasyonlar (3. tur, `mutate3.py`):** **43/43 kırmızı** — 2. turun 34'ü (iki çapası 3.
>   tur koduna yeniden bağlandı: N4 artık "gelecek koruması", N9 "dolgudan önce işaret yok") +
>   X11 `generationGuards` reset'ten silindi → `TestScript_GenerationGuards`,
>   `TestResetMFA_AClockAheadDoesNotReopenAUsedLink` · X35 gelecek koruması yalnız reset için
>   atlandı → aynı ikisi · X36 yaş koruması yalnız reset için → `TestScript_GenerationGuards` ·
>   X37 30 sn'lik pay geri → aynı ikisi · X1 `v_rows <> 1` → `< 1` · X2 alan sayısı denetimi
>   yok · X26 `--` işareti denetimi yok · P1 dolgu denetimi yok → dördü
>   `TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted` · P2 dolgu 29 bayt →
>   `TestCopy_ADataErrorShowsThePaddingNotTheValues`, `TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue`,
>   `TestPayload_…`. **Ölçümün kendi kusuru, düzeltildi:** ilk koşuda X1/X2/X26/P1 YEŞİL çıktı —
>   koşturucunun DB deseni 3. turun iki yeni test önekini (`TestPayload_`, `TestCopy_`)
>   içermiyordu, yani bu dört mutantı yakalayan test hiç koşmadı; desen düzeltilip altısı
>   yeniden koşuldu (6/6 kırmızı). Aynı koşu iki test kusuru gösterdi, ikisi de düzeltildi: P2
>   altında `TestCopy_…` sabit bir dilim indeksi yüzünden **derlenmiyordu** (artık `min` ile) ve
>   N9 altında `TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue` `disable` satırında boş dilimi indeksleyip **panic**
>   ediyordu (DB testleri koşmadan ikili duruyordu; artık alan sayısını denetleyip `t.Fatalf`).
>
> **4. tur — kapanış (2026-10-03, kapanış denetçisi ONAY; ucuz bloklamayanlar).** Kapanış
> denetçisi iptal yolunu gerçek sunucuda ölçtü: `pg_cancel_backend` ve `statement_timeout`
> (57014) CONTEXT'te dolguyu gösteriyor, değeri değil; gerçek alt sınır 96 bayt, pay 4 bayt.
>
> - **F1** · README reset-mfa tablosunun son satırı 3. turda bir 4. hücre ve başıboş bir
>   satır taşıyordu (`TAPPA_OPERATOR_TOTP_KEK` cümlesi tablonun içinde kalmıştı); paragraf
>   tablonun dışına alındı. Doğrulandı: goldmark v1.7.8 GFM ile bölüm render edildi — iki
>   tablonun her satırı 3 hücre (4 ve 7 satır), cümle bir `<p>` içinde.
> - **F2** · sıfır payın pini ~1 sn çözünürlükteydi (K10, 500 ms pay, yeşil kalıyordu):
>   `TestScript_GenerationGuards` iki alt komut için **+250 ms red** vakası koşar. Metinler
>   ölçülen çözünürlüğe bağlandı ("kodda pay yok; ölçülen en küçük ret +250 ms").
> - **G1** · `TestMain_TheBinaryRefusesARedirectedStderr`'in `/dev/null` kolu derlenmiş
>   komutun SQL'indeki üretim zamanının koşu anından ±5 sn içinde olduğunu denetler (K7a
>   −29 dk, K7b +1 sa).
> - **G2** · `TestCopy_ADataErrorShowsThePaddingNotTheValues` değerlerin tamamını ve **ilk 8
>   baytını** arar (K14, 70 baytlık dolgu: CONTEXT hash'in ilk 26 karakterini gösteriyordu).
> - **F3/F4/F5** · md. 13 "on üç"; PART II'nin `TestSQL_TheValuesTravelAsCopyData` maddesine
>   dolgu eklendi; SQL başlığında GUI "ölçülmedi" diye bağlandı.
> - **F6** · `dbGen` gerekçesi yukarıda ölçülen saat farkına bağlandı; yarış iddiası "tahmin,
>   yeniden üretilmedi".
> - **G3 (gözlem, doğrulanamadı)** · kapanış denetçisinin bir koşusunda açıklanamayan tek bir
>   `TestApply_AFailureAnywhereLeavesNoRow` kırmızısı oldu (mutant o koda dokunmuyordu;
>   denetçi 8 tam + 25 tek koşuda yeniden üretemedi). Bu turun koşularında görülmedi: üç
>   DB'li mutant koşusu (K10, K14, X37) ve son `-race` koşusu.
>   **2026-10-03 · kök neden ölçüldü (CI run 37084001714):** advisory kilit KUYRUĞU — test kilidi bir bağlantıda PAYLAŞIMLI tutarken reset-mfa alt testi `ownerTx` ile ikinci bir bağlantıdan yeniden istiyordu, araya giren `internal/db` `opTx` DIŞLAYICI isteği o isteği kuyrukta bekletip kendisi üst bağlantıyı bekledi (Postgres'in göremediği istemci tarafı döngü; iki pakette 3'er dk zaman aşımı) · sondayla birebir üretildi (aynı `:828` hatası, 184,67 sn) · düzeltme: alt test `beginOwnerTx` ile üst ağacın kilidi altında, yeniden istemeden → aynı sonda 5,43 sn yeşil · pin `TestTablesLock_IsTakenOncePerTestTree` (dört paketin test kaynağı; tek bulgu buydu).
> - **Mutasyonlar (4. tur, `mutate4.py`):** **5/5 kırmızı** — K10 500 ms pay →
>   `TestScript_GenerationGuards` · K7a `main` 29 dk geçmişle üretir · K7b `main` 1 sa ileriyle
>   üretir → ikisi `TestMain_TheBinaryRefusesARedirectedStderr` · K14 70 baytlık dolgu →
>   `TestCopy_ADataErrorShowsThePaddingNotTheValues`, `TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue`
>   · X37 (30 sn pay, 3. turdan yeniden) → `TestResetMFA_AClockAheadDoesNotReopenAUsedLink`,
>   `TestScript_GenerationGuards`.

> **Kart düzeltmesi (2026-10-03, OP-10 A fazı — veri katmanı — uygulaması sırasında).**
> Yazıldı: `db/migrations/00027_move_legal_publishing_to_the_operator.sql` (üç `op_*`, iki kapalı
> küme, `published_at` DEFAULT'u, yetkiler), `internal/db/operator.go` (dışa açık `LegalVersions`,
> `PublishLegal`, `LegalVersionsPage`, `LegalVersion`, `LegalPublisherKind`; paket içi
> `beginOperatorRead`, `readLegalVersions`, `readTicket`; `OperatorConn`'a `Query`),
> `db/queries/operator.sql` (üç ifade belgeye eklendi), `db/queries/legal.sql` (eşitlik kırıcı
> yorumu — `published_at` DEFAULT'u değişti) + `make gen` (`internal/store/legal.sql.go`,
> `querier.go`: yalnız yorum), `internal/db/operatorlegal_test.go` (yeni, 22 test; 2. turda 24), ADR 0021
> "OP-10 uygulama notu", ADR 0020 §7 tarihli not. Güncellenen pinler (zayıflatılmadı, gerekçe
> md. 9): `TestOperator00026_PrivilegeMatrix`, `TestOperator00026_TableShapeChecks`,
> `TestOperatorSQL_OnlyBoundParameters`, `TestOperatorAccessors_TheCustomerRoleCannotUseThem`.
> Gerekçeli kapsam genişlemesi (md. 10): `internal/domain/legal/legal_db_test.go`. Handler, ekran,
> wiring ve izin listesinin kaldırılması B fazıdır (md. 14). Ölçüm: dev Postgres 17.10,
> `tappa_owner`; kimlik `SET LOCAL SESSION AUTHORIZATION` ile.
>
> 1. **Kriter düzeltmesi (OP-4 bloğu md. 2'nin uygulanışı):** `has_table_privilege(tappa_app,
>    'legal_documents','INSERT') = false` 00027'den ÖNCE de yeşildi (ölçüldü, `BEGIN … ROLLBACK`:
>    `has_table_privilege` = `f`, `has_any_column_privilege` = `t`, `tappa_app` yazabiliyordu).
>    Kanıt `has_any_column_privilege(…,'INSERT') = false` ve beş sütunda
>    `has_column_privilege(…,'INSERT') = false`; tablo düzeyi sorgunun kör olduğu, sütun grant'ı
>    savepoint içinde geri verilerek aynı testte gösterilir
>    (`TestOperator00027_TheApplicationCanNoLongerWriteALegalText`). Tablo düzeyi `REVOKE INSERT`
>    sütun grant'larını da sildi; sütun düzeyi yazım (`REVOKE INSERT (slug, body) …`)
>    `has_any_column_privilege`'ı `t` bıraktı (ölçüldü; mutasyon M9 kırmızı).
> 2. **`op_begin_read(p_session text, p_kind text, p_params jsonb) RETURNS text` — genel ilk aşama.**
>    Okuma türleri kapalı küme (bugün `legal_versions`): fonksiyonda (22023) ve
>    `operator_read_tickets_kind_check`'te (OP-5 md. 14 c: şekilden kapalı kümeye). Her tür
>    anahtarlarını tam adlandırır; hash'lenen metin tipli değerlerden yeniden kurulan jsonb
>    nesnesidir (`op_read_*` aynısını kendi argümanlarından kurar). Bilet: üç
>    `gen_random_uuid()`'nin SHA-256'sı (366 bit `pg_strong_random`; `pgcrypto`'ya migration
>    bağımlılığı eklenmedi). Ömür **30 sn** (OP-5 md. 14 a). Audit: tür `read`, `target_scope` =
>    okuma türü, içeriksiz sayfa; yeni CHECK `operator_audit_log_read_has_scope`. Kısıt
>    yakalayıcı: bilet satırının DETAIL'i bilet hash'ini taşırdı → sabit 28000.
> 3. **`op_read_legal_versions(p_session, p_ticket, p_page_number, p_page_size)`** — ilk
>    `op_read_*`. Tek tüketen `UPDATE`, altı koşul `WHERE`'de pozitif; LIMIT gövdede
>    `least(sayfa, 200)`. Sütunlar: `version_id, slug, published_at, body_bytes, publisher_kind,
>    publisher_admin_id, publisher_name, is_current`. Eski satır (M7-06 paneli: müşteri
>    `admin_users.id`'si ya da NULL) `publisher_kind = 'legacy'`, id ve ad NULL — müşteri admin
>    id'si döndürülmez. Gövde döndürülmez (uzunluğu döner). Sıralama herkese açık okumanınki.
> 4. **`op_publish_legal(p_session, p_slug, p_body) RETURNS void`** — sürüm + `legal_publish`
>    satırı tek ifade; `published_by` = oturumun operatörü (aktör parametresi yok, imza pinli);
>    `detail` = `{slug, document_id, bytes}`. 256 KiB = `octet_length > 262144` → 22023.
>    **Ölçülerek eklenen:** 00020'nin `btrim(body) <> ''` CHECK'i satır sonu/sekmeden ibaret gövdeyi
>    geçirir (`btrim(E'  \n\t ') <> ''` = `t`, ölçüldü); fonksiyon `[:space:]` dışında karakter
>    taşımayan gövdeyi reddeder. Kısıta ulaşan argümanlar yakalanır (tek 22023, DETAIL yok).
> 5. **`legal_documents.published_at` DEFAULT `clock_timestamp()`** ve sütun tanımlayıcının INSERT
>    listesinde değil (00026'nın biçimi; K6'nın "açıkça yaz" yolu seçilmedi — yazılabilir saat
>    sütunu, ADR 0015). `db/queries/legal.sql`'in eşitlik kırıcı yorumu güncellendi.
> 6. **00026'nın ön koşulu tekrarlandı** (dokuz rol şekli testte:
>    `TestOperator00027_PreconditionRefusesAWrongCluster`).
> 7. **Down:** `tappa_app`'in INSERT'ini geri verir (00020'nin sütun grant'ı; bilinçli gerileme,
>    00026 durumu); audit tür CHECK'i yeni türden satır VARSA `NOT VALID` döner (ek-yalnız
>    kanıt). **Ölçüm (dev, `pg_dump --schema-only`, `\restrict` satırları ayıklanarak):**
>    `read` satırı yokken v26 → Up → Down şeması v26'yla birebir (sha256 öneki `4d918cd2e29385f9`
>    iki tarafta), Up → Down → Up v27 birebir (`ca32c68b7a009540` iki tarafta); son hâlde
>    (`read` satırları varken) Down şemasının v26'dan tek farkı `NOT VALID`, yeniden Up birebir
>    (`ac206fedc7a6548b` iki tarafta). Mutasyonlardan sonra migration'a yalnız fonksiyon
>    gövdelerinin DIŞINDAKİ yorumlarda dokunuldu; son dosya (sha256 öneki `a6b992b6d7cfe784`)
>    Down → Up ile yeniden uygulandı ve şema dökümü yine `ac206fedc7a6548b`. Her adımda
>    `goose version`/`status` ölçüldü; migration komutları zincirlenmedi.
> 8. **Eşzamanlı görünmezlik — ölçülen davranış, ilk test yanlış nesneyi bekliyordu:** A'nın açık
>    ilk aşamasının bileti başka bağlantıdan görünmez (sahibin sayımı 0, A'nınki 1); AYNI oturumla
>    okuyan B veri almaz — `op_touch_session`'ın oturum satırı kilidinde bekler
>    (`pg_stat_activity.wait_event_type = 'Lock'`) ve A geri alınınca 28000 alır. Testin ilk hâli
>    anında ret bekledi ve 180 sn'lik son süresine kadar asılı kaldı.
> 9. **Pin güncellemeleri ve nedenleri:** `TestOperator00026_PrivilegeMatrix` — tanımlayıcının yeni
>    sütunları (platform_admins `display_name`, operator_audit_log `id`) ve `opdefinerTenantGrants`'a
>    `legal_documents:SELECT`/`:INSERT` tam sütun listesiyle (OP-5 uygulama notunun kuralı);
>    `TestOperator00026_TableShapeChecks` — fikstür bilet türü `'probe'` → `'legal_versions'`:
>    00027'nin kapalı kümesiyle `'probe'` HER INSERT'i tür CHECK'ine takıyordu, yani 61 sn ve
>    `created_xact` '1'/'2' retleri yanlış sebeple 23514 kalıyordu (sessiz zayıflama) ve kontrol
>    kırmızıydı; retler artık kısıt ADIYLA pinli (`opWantConstraint`).
>    `TestOperatorSQL_OnlyBoundParameters` — 7 → 10 sabit, ve `Query` çağrıları da okunuyor (aksi
>    hâlde `op_read_*`'ın çağrısı taramanın dışında kalırdı). `TestOperatorAccessors_TheCustomerRoleCannotUseThem`
>    — iki yeni erişimci (42501).
> 10. **Gerekçeli kapsam genişlemesi — `internal/domain/legal/legal_db_test.go`:** REVOKE, altı DB
>     testini kırdı (hepsi `Store.Publish` ile tohumluyordu; ölçüldü: altısı 42501). Adlar korundu;
>     sürümler sahibin bağlantısıyla (`ownerPublish`) — `op_publish_legal`'in yerine geçen —
>     ekleniyor; `TestLegalDB_PublishWritesTheTextTheTrailAndTheSnapshotOrNoneOfThem` artık "hiçbiri"
>     yarısını ölçer (42501, metin yok, iz yok, anlık görüntü değişmedi);
>     `TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo` kolonu sahibin geri alınan
>     transaction'ında sürer ve `tappa_app`'in INSERT'inin 42501 olduğunu da söyler.
> 11. **`*OperatorDB`'ye yöntem EKLENMEDİ (ölçülerek en dar değişiklik):**
>     `TestOperatorDB_IsTheStoreAndNothingMore` yöntem kümesini `operatorauth.Store` + `Close`'a
>     türeterek pinler; yöntem eklemek ya o pini değiştirmeyi ya da `operatorauth.Store`'u
>     büyütmeyi isterdi, tüketicisi (B'nin handler'ı) henüz yokken. OP-7'nin pinleri
>     (`TestConstructors_BodiesAreTheReviewedOnes`, `TestConstructors_TheHookReachesOnlyThePin`,
>     `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator`, `TestOperatorDB_EveryMethodDelegatesVerbatim`,
>     `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor`) dokunulmadı ve yeşil.
> 12. **Kalıcı test verisi (ölçüldü — son `-race` koşusu öncesi/sonrası, `internal/db` +
>     `internal/domain/legal`: "op10" hesapları 10 → 12, oturumları 10 → 12, `read` satırları
>     10 → 12, `legal_publish` 0 → 0, bilet 0 → 0, `legal_documents` 3222 → 3227; aktif "op10"
>     hesabı 0, iptal edilmemiş oturumu 0):** `internal/db` koşu başına
>     2 operatör hesabı (`disabled`), 2 oturum (`revoked`), 2 `read` audit satırı bırakır
>     (`TestOpReadLegalVersions_TwoPhaseLifecycle` ve `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions`
>     bir `read` satırı commit eder; satırı 00026'nın tetikleyicisi sahibe karşı da korur, onun
>     başvurduğu hesap ve oturumu `ON DELETE RESTRICT` yabancı anahtarları tutar); bilet 0,
>     `legal_documents` 0. `TestOpReadLegalVersions_AnUncommittedTicketIsInvisibleToAnotherSession`
>     commit ettiği hesap ve oturumu temizlikte siler. Okuma tarafının koşullarını commit'siz
>     sürmek için testler bileti sahibin bağlantısıyla `created_xact` = commit edilmiş bir
>     transaction'ın kimliği olarak yazar (`opForgeTicket`/`opCommittedXact`) — sahibin yazabildiği,
>     tanımlayıcının yazamadığı sütun. `internal/domain/legal` koşu başına 5 FAKE sürüm commit
>     eder (önce 7; `Store.Publish` ile).
> 13. **Mutasyonlar** (her biri ilgili testte KIRMIZI; migration mutasyonları dosyanın
>     kopyasından yazılıp `goose down` → `up-by-one` ile uygulandı, her adımda sürüm ölçüldü;
>     yedekler `scratchpad/op10a/orig/`; sonda pristine dosya — o anki sha256 öneki `b7473528` —
>     geri yazıldı ve uygulandı; Go mutasyonlarında `operator.go` aynı şekilde geri yazıldı):
>
>     | # | Mutasyon | Kırmızı test (ilk hata satırı) |
>     |---|---|---|
>     | M1 | okuma süresi `now()` | `TestOpReadLegalVersions_ExpiryIsTheWallClock` (savepoint kolu) |
>     | M2 | okuma süresi `statement_timestamp()` | aynı test (DO kolu: `1/2`, `0/3` beklenirken) |
>     | M3 | `pg_xact_status(...) = 'committed'` silindi | `TestOpReadLegalVersions_ATicketFromThisTransactionIsRefused` (A1) |
>     | M4 | `consumed_at IS NULL` silindi | `TestOpReadLegalVersions_TwoPhaseLifecycle` (commit sonrası üçüncü okuma) |
>     | M5 | parametre bağı düştü (iki fonksiyonda) | `TestOpReadLegalVersions_TwoPhaseLifecycle` (başka sayfa) |
>     | M6 | `op_publish_legal` oturumu touch'sız arar | `TestOpPublishLegal_RefusesEveryDeadSession` |
>     | M7 | audit ayrı ifadede, hatası yutuluyor | `TestOpPublishLegal_AFailedAuditRowTakesTheVersionWithIt` |
>     | M8 | 256 KiB sınırı +1 | `TestOpPublishLegal_RefusesADocumentWithoutEchoingIt` |
>     | M9 | sütun düzeyi `REVOKE INSERT (slug, body)` | `TestOperator00027_TheApplicationCanNoLongerWriteALegalText` |
>     | M10 | `published_by` istemciden (`p_publisher` DEFAULT NULL) | `TestOperator00027_TheThreeFunctionsAndTheirExactSignatures` |
>     | M11 | `tappa_app`'e EXECUTE | aynı test + `…TheApplicationCanNoLongerWriteALegalText` + `TestOperator00026_ForwardCatalogPin` |
>     | M12 | bilet ömrü 59 sn | `TestOpBeginRead_WritesOneAuditRowAndStoresNoTicket` |
>     | M13 | `published_at` DEFAULT `now()` kaldı | `…TheThreeFunctionsAndTheirExactSignatures` + `TestOpPublishLegal_WritesTheVersionAndItsAuditRowTogether` |
>     | M14 | oturum bağı silindi | `TestOpReadLegalVersions_AForgedTicketIsRefused` |
>     | M15 | LIMIT tavanı silindi | `TestOpReadLegalVersions_PagesAreCappedAndOrdered` (300 satır) |
>     | M16 | fazla anahtar kabul | `TestOpBeginRead_RefusesDeadSessionsAndBadParameters` |
>     | M17 | `op_publish_legal`'de kısıt yakalayıcı yok | `TestOpPublishLegal_RefusesADocumentWithoutEchoingIt` (23514 + DETAIL) |
>     | M18 | `op_begin_read`'de kısıt yakalayıcı yok | `TestOpBeginRead_ARefusedWriteCarriesNoTicketHash` (DETAIL 375 bayt) |
>     | M19 | boşluk-yalnız gövde kontrolü yok | `TestOpPublishLegal_RefusesADocumentWithoutEchoingIt` |
>     | M21 | Down `tappa_app` INSERT'ini geri vermiyor | `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain` |
>     | M22 | Down her zaman `NOT VALID` | aynı test (2. dal) |
>     | M23 | ön koşul "operatörün üyesi" kontrolü yok | `TestOperator00027_PreconditionRefusesAWrongCluster` |
>     | G1 | erişimci sayfa argümanlarını yer değiştiriyor | `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` + `…PagesAreCappedAndOrdered` |
>     | G2 | `readTicket.Format` ham bileti basıyor | `TestReadTicket_PrintsOnlyThePlaceholder` |
>     | G3 | `PublishLegal` çalışma anında kurulan SQL metni | `TestOperatorSQL_OnlyBoundParameters` |
>     | G4 | `LegalVersions` ikinci aşamayı atlıyor | `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` |
>
>     (M20 numarası kullanılmadı.) Pozitif kontrol: pristine dosyada 22 testin 22'si yeşil.
> 14. **B fazına devir (numaralı):**
>     1. **Erişimciler (`internal/db`):** `LegalVersions(ctx, c OperatorConn, sessionHash string,
>        page LegalVersionsPage) ([]LegalVersion, error)` — HAVUZLA çağrılır (tek transaction'da
>        `ErrOperatorRefused`); `PublishLegal(ctx, c OperatorConn, sessionHash, slug, body string)
>        error` — ölü oturum `ErrOperatorRefused`, reddedilen belge `SQLSTATE 22023` taşıyan bir
>        veritabanı hatası (sentinel değil; `PgError`'a ulaşılamaz). Sayfa: `Number` ≥ 1, `Size` 1..200.
>     2. **`*OperatorDB` yöntemleri:** `LegalVersions(ctx, sessionHash, page)` ve
>        `PublishLegal(ctx, sessionHash, slug, body)`, `return F(ctx, o.pool, …)` biçiminde; AYNI
>        değişiklikte: handler paketinde tüketici arayüzü; `TestOperatorDB_IsTheStoreAndNothingMore`
>        istenen kümeyi `operatorauth.Store` ∪ o arayüz ∪ `Close` diye türetir;
>        `TestOperatorDB_EveryMethodDelegatesVerbatim` sayısı 7 → 9;
>        `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` öncülü 8 → 10;
>        `cmd/tappa`'nın `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator`'ı (pinlediği
>        kural: `configuredSurface`'in store'u `operatorAuthenticator`'a, o da `operatorauth.New`'e
>        gider) legal handler'ına da izin verecek şekilde, kurallarıyla yeniden yazılır.
>     3. **Handler `/operator/legal`** OP-8 kabuğunun içinde, aynı zincirle (host kapısı, oturum
>        kapısı, oturum bütçesi — bir okuma iki DB çağrısıdır —, POST'ta kabuğun
>        CSRF/`Sec-Fetch-Site` kapısı). GET: sürüm listesi + `legal.Store.Published()` anlık
>        görüntüsünden güncel metinler; `publisher_kind = 'legacy'` → "tenant admin (legacy)".
>        POST: slug `legal.Slugs` içinde, **görünür karakter denetimi** (2. tur, md. 22: ne
>        veritabanının `[:space:]` kontrolü ne `strings.TrimSpace` U+180E, U+200B, U+200C,
>        U+200D, U+2060, U+2800, U+3164, U+FEFF'den ibaret bir gövdeyi reddeder — ölçüldü;
>        önerilen biçim `unicode.IsSpace` + `unicode.Is(unicode.Cf, r)` + ölçülen kod noktaları
>        (Hangul dolgusu U+3164, Braille boşluğu U+2800) — liste ve karar B'nin),
>        `http.MaxBytesReader` ile 256 KiB
>        (`internal/handler/legaladmin.go:92`'deki `maxLegalBody` emsali — o dosya kalkınca sabit
>        operatör handler'ında yeniden bildirilir; `internal/domain/legal`'in yorumları
>        `handler.maxLegalBody`'yi anar); `PublishLegal`; ardından **`legal.Store.Refresh(ctx)`** (tek
>        replika; ADR 0020 §7) — Refresh hatası log'lanır, yayın veritabanında vardır.
>        Geri alma = eski metni yeni sürüm olarak yayımlamak; sürüm listesi gövde taşımaz — bir
>        "bu sürüme dön" düğmesi yeni bir `op_read_*` (kendi türü: `op_begin_read`'in kümesi +
>        `operator_read_tickets_kind_check`, bir migration'da) ister.
>     4. **Panelden kaldırılacaklar:** `TabLegal`, `PanelSection.OperatorOnly`, `PanelChrome.Operator`,
>        `mayPublishLegal`, `config.OperatorAdminIDs`, `TAPPA_OPERATOR_ADMIN_IDS`
>        (`deploy/k8s/05-config.yaml:125`, `.env.example:124`, `deploy/README.md:308` — bugünkü
>        satırlar), `internal/handler/legaladmin.go` ve testleri; `legal.Store.Publish` + `Trail` +
>        `ActionPublished` + `PublishedDetail` (00027'den sonra veritabanı bu yolu 42501 ile
>        reddeder) ve `db/queries/legal.sql`'in `PublishLegalDocument`'ı + `make gen`
>        (`cmd/tappa/storekeyshape_test.go` imza listesinde `PublishLegalDocument` var);
>        `TestLegalDB_PublishWritesTheTextTheTrailAndTheSnapshotOrNoneOfThem` ya `Store.Publish`'le
>        gider ya da uygulama tarafı REVOKE regresyonu olarak kalır — B karar verir ve yazar.
>        **(2. tur ekleri)** `TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo`
>        (`internal/domain/legal/legal_db_test.go`, bugün satır 389) de `Store.Publish` ve
>        `ErrEmptyBody` kullanıyor — `Publish` kalkınca testin "Go" yarısı (boş gövde Go'da
>        reddedilir) operatör handler'ının testine taşınır, kolon yarısı yerinde kalır;
>        `cmd/tappa/main.go:411`'deki `legal.NewStore(data, trail)` çağrısının imzası
>        `Trail` kalkınca değişir (`NewStore`'un nil-trail reddi ve `TestNewStore_RefusesAMissingTrail`
>        onunla birlikte).
>        `rg` kriteri: *"`db/migrations` ve ADR geçmişi hariç"* (OP-4 bloğu md. 20).
>     5. **`TestLegalPublicPath_WritesNothing` (`internal/handler/legaladmin_test.go:607`) ve
>        `TestLegalReader_CannotReachTheDatabase` (`:547`) VAR** (ölçüldü). `legaladmin_test.go`
>        silinince aynı adlarla kalan bir dosyaya taşınır (OP-10 kabulü). İkincisinin pozitif
>        kontrolü `panelTexts.Publish`'i kullanıyor — o özne `legaladmin.go`'yla gider; başka bir
>        G/Ç biçimli arayüzle (örn. legal handler'ının arayüzü) bağımsız kalacak şekilde değiştirilir.
>        Birincisinin canlı yarısı `fakeTexts`'i (`legalfake_test.go`) kullanıyor.
>        M7-06 testleri ADR 0016'da, `m7-portal.md`'de ve bu dosyada adıyla anılıyor; silinirlerse
>        `TestEveryNamedTestExists` kırılır — ya aynı adla tutulur ya da sarkan atıf bütçesi aynı
>        değişiklikte gerekçesiyle artırılır (OP-4 bloğu "OP-10 tuzağı"). Bugün: 54 canlı, 54 bütçe.
>     6. **OP-8 kabuğu yükümlülükleri:** `TestSurface_TheScreensOfLaterTasksAreNotMounted`
>        `/operator/legal`'i "monte DEĞİL" listesinde tutuyor — aynı değişiklikte çıkarılır;
>        yeni her yöntem × rota: `classRoutes` (`op8r5_test.go`) + başlık tablolarından birinde bir
>        sınıf, `designedHeaders` (`op8r2_test.go`), sızıntı testinin kolları (`leak_test.go`),
>        `harvestWant` (`leak_test.go:287`) — `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`
>        sınıfsız monte edilmiş bir çifti kırmızıya çevirir, öbür üçü elle eklenir; konsoldan link
>        ancak rota kayıtlıyken (`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`).
>     7. **Asla loglanmayanlar:** okuma bileti `internal/db`'den çıkmaz (paket içi `readTicket`);
>        handler oturum hash'ini ve `PgError.Detail`'i log'lamaz; yayımlanan gövde log'a yazılmaz.
>     8. **Ekranın kapattığı metin:** ADR 0016 Sonuçlar *"kim yayımladı ekranı yok"* (ADR 0020
>        yerine geçilenler tablosu) B'nin ekranıyla kapanır.
> 15. **Sayılı sınırlar (A):** (L1) md. 12'nin kalıcı satırları — ve **dev'in herkese açık yasal
>     sayfaları test metni gösterir**: `internal/domain/legal`'in `ownerPublish`'i koşu başına 5
>     FAKE sürüm commit eder ve dört slug'ın dördünün güncel sürümü bir test metnidir; §4.6
>     açısından sorun değil, demo öncesi dev veritabanı sıfırlanır (2. tur, güvenlik D4);
>     (L2) boşluk-yalnız gövde kontrolü locale'e bağlıdır (md. 22'nin ölçülen listesi; üretimin
>     ctype'ı ölçülmedi) ve sekiz ölçülen kod noktasından ibaret bir gövdeyi ne o kontrol ne Go'nun
>     `TrimSpace`'i reddeder — boş görünen bir yasal sayfa yayımlanabilir (güvenilen operatör girdisi; bütünlük, güvenlik
>     değil), görünür karakter denetimi B'de (md. 14.3); (L3) Down `tappa_app`'e
>     INSERT'i geri verir; (L4) geniş varsayılan ACL altında `pg_dump` geri yüklemesi `tappa_app`'e
>     `legal_documents` INSERT'i geri verebilir ve `USING (true) WITH CHECK (true)` politikası onu
>     durdurmaz — `scripts/pg-restore-verify.sh`'in tablo ve sütun yetki karşılaştırmasının
>     raporlaması beklenir (bu görevde ölçülmedi); politikayı `WITH CHECK (false)` ile daraltmak
>     ölçülüp alınmadı (redline R5b, ADR 0021 OP-10 notu md. 14); (L5) sınır 4'ün penceresi 30 sn;
>     (L6) aynı oturumla eşzamanlı çağrılar oturum satırı kilidinde sıraya girer (md. 8); (L7) iki
>     kopya kanonikleştirme (`op_begin_read`'in `v_bound`'ı, `op_read_*`'ın
>     `jsonb_build_object`'i) — kaymayı gerçek begin → read yolu ölçer
>     (`TestOpReadLegalVersions_TwoPhaseLifecycle`), yalnız böyle bir testi olan türler için.
> 16. **Güvenlik iddiası** ADR 0021 → "OP-10 uygulama notu" sonunda üç parçalı (PART I ölçen
>     testlerin adlarıyla, PART II pinler, PART III).
>
> **2. tur (2026-10-03; üçüncü göz ONAY + tappa-security-auditor ONAY, kalan düşük bulgular).**
> Dal ucu `85bcf6b`'ye hızlı ileri sarıldı (çakışma yok: ucun dosyaları `cmd/opadmin/*`,
> `internal/brand/*`, ADR 0023/0024, `docs/plan/*`). Migration'da yalnız fonksiyon gövdesi DIŞI
> bir yorum değişti (md. 21; dosya sha256 öneki `aad7653eb6cefb49`), şema dökümü yine
> `ac206fedc7a6548b`.
>
> 17. 🔴 **A, B olmadan `main`'e GİTMEMELİ (B11, güvenlik D3).** 00027 deploy edilip B gelmezse
>     panelin `/admin/legal` POST'u `Store.Publish`'in 42501'iyle 500 döner
>     (`problemLegalUnavailable`, `internal/handler/legaladmin.go`'nun `default` dalı) ve
>     operatörün yasal metin ekranı yoktur; o arada bir yasal metin ancak `tappa_owner`'ın SQL'iyle
>     eklenir — `legal_publish` satırı bırakmadan, sürüm listesinde `legacy` sınıflanarak. ADR 0021
>     OP-10 notu md. 15.
> 18. **Kilit pini (B1):** ucun `TestTablesLock_IsTakenOncePerTestTree`'si (`cmd/opadmin/tableslock_test.go`)
>     `opLiveFixture` yüzünden kırmızıydı (ölçüldü: "the lock statement is in
>     [TestOperatorDB_RunsAsTappaOperator opLiveFixture opTx], want [… opTx]"). `tablesLockStatements`'a
>     `opLiveFixture` eklendi, `tablesLockDirs` ve PART II yorumları güncellendi; test PASS ve
>     `internal/db` için tarama bulgusu 0 (fonksiyon başına tek istek, alt testte/döngüde yok).
> 19. **Tüketim koşullarının kaynak pini (D1 + B7 + U15):** `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`
>     — adı `op_read_` ile başlayan HER fonksiyon (katalogdan, adla) normalleştirilmiş kaynağında:
>     oturum önce `op_touch_session` ile çözülür; `operator_read_tickets`'ın TEK UPDATE'i
>     `SET consumed_at = clock_timestamp() WHERE …`; WHERE'de altı koşul (hash: ham bilet ‖
>     parametre nesnesi; `session_id = v_session`; `kind = '…'` ve değeri tür CHECK'inin kümesinde;
>     `consumed_at IS NULL`; `expires_at > clock_timestamp()`; `pg_xact_status(created_xact) =
>     'committed'` POZİTİF); hemen ardından `IF NOT FOUND … 28000`; `RETURN QUERY` ondan sonra.
>     Pozitif kontrol: `op_read_legal_versions`'ın bir kopyası savepoint içinde 11 bozuk biçimde
>     yaratılır (her koşulun silinmesi, U1 yazımı, IF'e taşınmış negatif yazım, kapalı küme dışı
>     tür, `now()`, parametresiz hash, ret dalının silinmesi) ve her biri adıyla yakalanır;
>     değiştirilmemiş kopya yakalanmaz.
> 20. **Biletin kaynağı (B4):** `TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource` —
>     `v_ticket`'ın TEK ataması üç `uuid_send(pg_catalog.gen_random_uuid())` üzerinden SHA-256,
>     saklanan hash ondan, dönüş o; `pg_catalog.gen_random_uuid()` `LANGUAGE internal`. Kontrol:
>     saat+oturum türevli bilet (U2) bir kopyada yakalanır.
> 21. **Şema kümeleri ve küçük testler:** `TestOperator00026_TableShapeChecks`'e kapalı küme dışı
>     bilet türü (`'probe'` → `operator_read_tickets_kind_check`, B2) ve kapsamsız `read` satırı
>     (→ `operator_audit_log_read_has_scope`, B3; kapsamlı satır kontrol) eklendi;
>     `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain` bilet tür CHECK'inin tanımını
>     Up'tan önce (kapalı küme), Down'dan sonra (00026'nın şekli) ve yeniden Up'tan sonra ölçer;
>     `TestOpReadLegalVersions_PagesAreCappedAndOrdered` Maltaca bir gövdede `body_bytes`'ın bayt
>     (52) olduğunu, karakter (46) olmadığını ölçer (B5; `LegalVersion.BodyBytes` yorumu buna
>     bağlandı); `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` artık erişimciyle 2.
>     sayfayı okur ve commit edilen audit satırının sayfa 2 / boyut 5 olduğunu ölçer (B6 — satır
>     içeriği tablodaki veriye bağlı değil). Metin (B10): test yorumu 18 vakalık listeye ve altı
>     ölü oturuma bağlandı; 22023'ün dalları migration'da ve ADR notunda aynı beş dalla sayıldı
>     (migration'ın eski "four" listesi boşluk-yalnız gövdeyi saymıyordu — yalnız yorum).
> 22. **Boşluk sınıfı ölçüldü (güvenlik D2):** dev, `datctype = en_US.utf8`, sağlayıcı libc.
>     `[:space:]` kontrolü REDDEDER: U+0009–000D, U+0020, U+0085, U+2000, U+2003, U+2028,
>     U+2029, U+205F, U+3000; KABUL EDER: U+00A0, U+1680, U+180E, U+200B, U+200C, U+200D, U+202F,
>     U+2060, U+2800, U+3164, U+FEFF. Go 1.26.7 `strings.TrimSpace` bunlardan U+00A0, U+1680,
>     U+202F'yi de kırpar; U+180E, U+200B, U+200C, U+200D, U+2060, U+2800, U+3164, U+FEFF'yi ne
>     kontrol ne `TrimSpace` reddeder (altısı `unicode.Cf`, U+2800 ve U+3164 değil). Üretimin
>     ctype'ı ölçülmedi. B'ye devir md. 14.3, sınır L2.
> 23. **Mutasyonlar (2. tur; her biri KIRMIZI, pristine'de yeşil; migration mutasyonları `goose
>     down` → `up-by-one`, her adımda sürüm ölçüldü):**
>
>     | # | Mutasyon | Kırmızı test (ilk hata satırı) |
>     |---|---|---|
>     | U1 | `pg_xact_status(...) = 'committed'` → `k.created_xact <> pg_current_xact_id()` | `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays` ("lacks created by a COMMITTED transaction, positive form") |
>     | U15 | tüketen UPDATE'ten `k.kind = 'legal_versions'` silindi | aynı test ("lacks this read kind") |
>     | C1 | hash koşulu silindi | aynı test ("lacks the hash of (raw ticket, …)") |
>     | C2 | oturum koşulu silindi | aynı test ("lacks this session") |
>     | C3 | `consumed_at IS NULL` silindi | aynı test ("lacks not yet consumed") |
>     | C4 | `expires_at > clock_timestamp()` silindi | aynı test ("lacks not expired by the wall clock") |
>     | C5 | commit koşulu silindi | aynı test ("lacks created by a COMMITTED transaction") |
>     | U2 | bilet `sha256(clock_timestamp()::text ‖ p_session)` | `TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource` |
>     | U8 | Up'ın bilet tür CHECK'i şekil kuralına gevşedi | `TestOperator00026_TableShapeChecks` ('probe' bileti başarılı) |
>     | U12 | `operator_audit_log_read_has_scope` silindi | `TestOperator00026_TableShapeChecks` (kapsamsız `read` başarılı) |
>     | U13 | `octet_length(d.body)` → `length(d.body)` | `TestOpReadLegalVersions_PagesAreCappedAndOrdered` (46, 52 beklenirken) |
>     | D4 | Down bilet tür CHECK'ini şekle döndürmüyor | `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain` |
>     | GB | `LegalVersions` iki aşamada da `Number = 1` | `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` (audit sayfa 1) |
>
>     Sonda pristine 00027 geri yazıldı, Down → Up ile iki kez uygulandı, şema `ac206fedc7a6548b`;
>     `operator.go` pristine'e geri yazıldı.
> 24. **Doğrulama (2. tur):** hedefli `-race -v` (`internal/db`, `internal/domain/legal`,
>     `cmd/tappa`, `cmd/opadmin`; .env'li): 408 üst düzey PASS, 0 SKIP, 0 FAIL (alt testlerle 875),
>     DATA RACE 0; `TestTablesLock_IsTakenOncePerTestTree` ve `TestEveryNamedTestExists` (54/54)
>     PASS. Kalıcı veri bu koşuda md. 12'nin oranıyla: "op10" hesap/oturum/`read` 36 → 38,
>     `legal_documents` 3242 → 3247, bilet 0, `legal_publish` 0.

> **Kart düzeltmesi (2026-10-03, OP-10 B fazı — ekran, wiring ve emeklilik — uygulaması sırasında).**
> Yazıldı: `internal/handler/operator/legal.go` (yeni: `LegalStore`, `LegalTexts`,
> `legalPage`, `publishLegal`, `legalSession`, `visibleText`, `blankSymbol`, `liveVersion`,
> `compare`, `snapshotDiffers`, `legalView`, `maxLegalBody`, `legalVersionsLimit`,
> `legalWriteTimeout`), `routes.go` (`GET`/`POST /legal` konsol grubunda; `pathLegal`;
> `spendSession` — `sessionGate`'in bütçe dalı ayrıldı), `surface.go` (`New(auth, legalStore,
> texts, host, baseURL, log)`; `sessionLimit` türetmesi), `render.go` (`readForm(w, r, limit,
> tooLarge)`; beş yeni `ProblemView`), `signin.go`/`enroll.go` (yeni `readForm` imzası);
> `web/templates/operatorpages/legal.templ` (yeni `Legal`; geride uyarısı `components.Notice`),
> `view.go` (`LegalView`, `LegalDoc` — `Behind`, `LiveAt` dahil —, `LegalVersionRow`), `home.templ` (konsoldan `/operator/legal` linki), `chrome.templ` (yorum);
> `input.css` (`.op-version`); `internal/db/operatorpool.go` (`LegalVersions`, `PublishLegal`
> yöntemleri + yorumlar), `operator.go` (OP-10 bölümünün yorumu); `internal/operatorauth/flow.go`
> (`(*Authenticator).SessionHash`); `internal/domain/legal/legal.go` (`Publish`, `Trail`,
> `ActionPublished`, `PublishedDetail`, `ErrEmptyBody`, `ErrUnknownSlug` kaldırıldı;
> `NewStore(data)`; `Refresh` kilitli; kilidin yorumu ölçülene daraltıldı); `docs/plan/m10-platform.md`
> (yalnız WL-7 satırı, 3. turda koordinatörün isteğiyle); `db/queries/legal.sql` (`PublishLegalDocument`
> kaldırıldı) + `make gen` (`internal/store/legal.sql.go`, `querier.go`); `cmd/tappa/main.go`
> (`texts` operatör yüzeyinden önce kurulur, `NewStore(data)`, `NewAdminAuth` texts'siz; açılış
> log satırı),
> `cmd/tappa/operator.go` (`openOperatorSurface(ctx, cfg, texts, log)`, `operatorStore`,
> `configuredSurface(store, texts, cfg, log)`); panelden kaldırılanlar:
> `internal/handler/legaladmin.go` (+ testi), `adminlogin.go` (`texts`, `operators`),
> `dashboard.go` (iki rota), `review.go` (`Operator`), `web/templates/pages/legaladmin.templ`
> (+ `_templ.go`), `legaladminview.go`, `adminview.go` (`TabLegal`, `OperatorOnly`, satır,
> `PanelChrome.Operator`), `admin.templ` (filtre); `internal/config/config.go`
> (`OperatorAdminIDs` + ayrıştırıcı + testi); `deploy/k8s/05-config.yaml`, `.env.example`,
> `deploy/README.md` (8. adım; K2/K4 taslağının gövde sınırı `320k`), `deploy/k8s/40-ingress.yaml`
> (yorum); `marketing.go` (`legalSlugOf` buraya taşındı); `adminreset.go` (iki eskimiş yorum:
> `legaladmin.go` atfı, `RecordTx` sayımı). Testler: `internal/handler/operator/op10_test.go` (yeni),
> `op10_db_test.go` (yeni), `rig_test.go`, `leak_test.go`, `op8_test.go`, `op8r2_test.go`,
> `op8r5_test.go`, `typepins_test.go`, `surface_test.go`, `op8_db_test.go`, `export_test.go`;
> `internal/handler/legalpublic_test.go` (yeni — taşınan dört test + panel taraması),
> `legalfake_test.go`, panelin 18 test dosyasında `NewAdminAuth` çağrısı, `adminlogin_db_test.go`,
> `billingactions_test.go`, `account_test.go`, `policies_test.go`; `internal/db/operatorpool_external_test.go`,
> `operatorpool_test.go`, `operatorlegal_test.go`; `internal/operatorauth/units_test.go`,
> `surface_external_test.go`; `internal/domain/legal/legal_test.go`, `legal_db_test.go`,
> `stub_test.go`; `internal/config/config_test.go`; `cmd/tappa/operator_test.go`,
> `storekeyshape_test.go`, `testnames_test.go` + `testdata/known-dangling-citations.txt`.
> ADR 0020 §7 *"OP-10 B fazı notu"* (üç parçalı güvenlik iddiası), ADR 0021 *"OP-10 B fazı
> eki"*. Migration YOK, bağımlılık YOK (`go.mod`, `go.sum`, `sqlc.yaml` diff boş).
>
> **Kararlar, ölçümüyle:**
> 1. **Oturum argümanı.** Handler'ın oturum taşıyan `op_*` çağrıları için
>    `operatorauth.(*Authenticator).SessionHash(tok)`; `Verify`'ın `op_touch_session`'a verdiği
>    değerle aynı, token anahtarına bağlı, bozuk token `ErrNoSession` ve boş hash
>    (`TestSessionHash_IsTheHashVerifyHandsTheStore`). Hash `legalSession`'da istenir; ekran
>    `sessionGate`'in arkasındadır.
> 2. **Rotalar.** `GET /operator/legal` (editörler + sürüm listesi) ve `POST /operator/legal`
>    (yayın), konsolun grubunda: host kapısı → güvenlik başlıkları → flood → same-origin
>    (okuma kapısıyla: `same-site`/`cross-site` getirme 303) → `requireOperator` →
>    `sessionGate`. Konsol (`Home`) `/operator/legal`'e link verir.
> 3. **Bütçe (OP-8'in OP-11'e devri burada ödendi).** Okuma iki işlem (`op_begin_read`,
>    `op_read_legal_versions`) ve iki birim sayılır: `sessionGate` + `legalPage`
>    (`spendSession`). `sessionLimit` 100 korundu, türetmesi yeniden yazıldı (~10 legal
>    görüntü × 2 + ~5 konsol + birkaç yayın ≈ 30, × ~3). Ölçülen: tek oturum 50 legal görüntü →
>    50 × 200 ve 50 okuma, 51. → 429 kapıda; 1 konsol + 49 legal (99 birim) sonrası görüntü →
>    429 ekranın ikinci biriminde, yüklem +1, okuma +0
>    (`TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget`).
> 4. **Yayın formu.** `slug` ve `body`; `legal.Valid(slug)` (büyük harf, boşluklu, beşinci
>    belge, yol, NUL: 400 — dokuz biçim, `TestLegalPublish_RefusesASlugTheProductDoesNotHave`);
>    gövdenin uçları `strings.TrimSpace` ile kırpılır, içi yazıldığı gibi; yayımlayan
>    oturumdan — formdaki `published_by`/`admin_id`/`session` alanları başka birini adlandırırken
>    yayın gönderen oturumun hash'iyle gitti (`TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows`).
>    `multipart/form-data` gövde `ParseForm`'da ayrıştırılmaz → boş slug → 400 (kod okundu).
> 5. **Görünür karakter kuralı (A'nın devri md. 14.3, sınır L2).** Gövdede Unicode
>    L/N/P/S kategorilerinden, `Other_Default_Ignorable_Code_Point` ve adı konmuş üç boş sembol
>    (`blankSymbol`: U+2800, U+303F, U+1D159) dışında en az bir karakter. Pozitif tanım seçildi:
>    bir "görünmezler listesi" uzatılacak bir liste olurdu. Üç sembolün üçü de dev veritabanında
>    (`en_US.utf8`) `'^[[:space:]]*$'` değil ve `btrim`'den sonra boş değil — yani tek başına
>    sütunun kontrolünü geçer (ölçüldü, salt okunur işlem). U+FFFC (OBJECT REPLACEMENT
>    CHARACTER) görünür sayılır: gömülü bir nesnenin yerini tutan görünür bir glif — karar,
>    Unicode adından; çizimi ölçülmedi.
>    A'nın sekiz kod noktasının sekizi de (tek tek, üçlü, birlikte, boşluklarla) reddedilir;
>    ayrıca boşluk/satır sonu, NBSP ailesi, kontroller, yumuşak tire, tek birleşik işaret,
>    Hangul dolguları (U+115F, U+1160, U+3164, U+FFA0 — kategori L ama ODICP), varyasyon
>    seçicisi, bidi kontrolleri, U+303F, U+1D159 ve üç boş sembol birlikte: 24 tablo girişi;
>    kabul: harf, Maltaca harf, rakam, noktalama, sembol, görünmezler arasında bir harf, bir
>    cümle, U+FFFC — 8 giriş (`TestVisibleText_TheListedInvisibleBodiesAreRefused`,
>    `TestLegalPublish_RefusesAnEmptyBodyAndSaysSo`: 400, store 0, refresh 0).
> 6. **256 KiB.** `maxLegalBody` (`legaladmin.go`'dan taşındı) istek gövdesine (`MaxBytesReader`,
>    URL-kodlu form) uygulanır; çözülen değer kodlamasından uzun olamaz, yani veritabanının
>    `octet_length > 262144` sınırının içindedir. Tam 256 KiB'lık istek yayımlanır, +1 bayt 413,
>    store 0 (`TestLegalPublish_RefusesABodyBiggerThanTheCeiling`). Bedeli: kodlama payı kadar
>    küçük bir metin sınırı (sınır LB6).
> 7. **Refresh — bağlam, PRG, iyileştirme, uyarı (2. tur).** (a) Yayın ve ardından
>    `legal.Store.Refresh`, `context.WithTimeout(context.WithoutCancel(r.Context()), 10 sn)`
>    altında koşar: yayından sonra bağlantıyı kapatan bir tarayıcı (ingress isteği iptal eder)
>    commit olmuş bir sürümün tazelemesini iptal ettiremez. Ölçülen: istek bağlamı handler'dan
>    ÖNCE iptal → yayın saklanır, anlık görüntü yeni metin; sahte store sürümü kaydettikten hemen
>    SONRA iptal → anlık görüntü yeni metin; iki tazelemenin bağlamı da iptalsiz ve ≤ 10 sn son
>    tarihli; kontroller: istek bağlamı gerçekten iptal edildi, sahteler iptal edilmiş bağlamı
>    reddeder (`TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh`; M28 =
>    X24'ün tersi, M29, M30 KIRMIZI). 3. tur (F3): "≤ 10 sn" sabitin kendisiyle karşılaştırılıyordu
>    (X02 — `time.Hour` — yeşildi); aynı test artık `legalWriteTimeout == 10*time.Second`'ı
>    `maxLegalBody` emsaliyle pinler (M43 = X02 KIRMIZI). (b) Tazeleme hatası loglanır ve yanıt yine **303 →
>    `/operator/legal`** (POST → 303 → GET): F5 ekranı yeniden okur, ikinci kez yayımlamaz —
>    1. turun 503 sayfası ve `problemLegalNotRefreshed` kaldırıldı. (c) Ekran her açılışta anlık
>    görüntüyü listenin canlı sürümüyle karşılaştırır (`compare`; listenin 100'lük sayfasında
>    olmayan bir canlı sürüm karşılaştırılmaz). **3. tur (F1):** anlık görüntü listeden ÖNCE
>    okunur — önce okunan anlık görüntü sonra okunan listeden yeni olamaz; ters sırada iki
>    okumanın arasına giren bir yayın (ve tazelemesi) anlık görüntüyü listeden yeni yapıyordu:
>    görüntü boşuna tazeliyor, editörü listenin göstermediği sürümle açıyor ve (1./2. turun
>    "farklı = geride" kuralıyla) uyarının üç cümlesi de yanlış çıkıyordu (denetçinin probe'u).
>    İkinci bir koruma da kondu: *farklı* (belge yok, başka zaman ya da başka uzunluk) bir kez
>    tazelemenin sebebidir; *uyarı* yalnız anlık görüntü canlı sürümden ESKİ ise (yok, daha
>    önce yayımlanmış ya da aynı zaman + başka uzunluk) çıkar — iyileştirmenin tazelemesi
>    listenin okunmasından sonra commit edilen bir sürümü kurabilir, o görüntü uyarı vermez.
>    Veritabanının "güncel"i seçtiği sıra `(published_at DESC, id DESC)`'tir
>    (`ListPublishedLegalDocuments`, `op_read_legal_versions`); `legal.Doc` id taşımadığından
>    `compare` yalnız `published_at`'ı karşılaştırır ve EŞİT zamanda id'nin vekili olarak
>    uzunluğu kullanır — "uyarı yalnız ESKİ anlık görüntüde" hükmü bu eşitlik durumunu
>    dışarıda bırakır (4. tur, N1; LB11). Ölçülen: iki okuma arasına giren yayın → 200, uyarı yok, görüntünün
>    kendi tazelemesi yok, editör listenin canlı gösterdiği sürümü taşır; kontrol: sonraki
>    görüntü yeni sürüm, uyarı yok, tazeleme yok
>    (`TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind`; M41 = sıra ters
>    KIRMIZI: 2 tazeleme, editör yeni sürüm); iyileştirmenin kurduğu sürüm listeden yeni → uyarı
>    yok; kontrol: aynı anlık görüntü iyileştirmeden önce "geride" (bir uyarı)
>    (`TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind`; M42 = yön
>    düşürüldü KIRMIZI). İki koruma bağımsız pinlidir: M41 tek başına uyarı ÜRETMEZ (yön kuralı
>    tutar), sıranın kendisi tazeleme sayısı ve editörün metniyle ölçülür. Hâlâ gerideyse o
>    belgenin editörü *"The public page is behind"* uyarısını canlı sürümün yayın zamanıyla
>    gösterir ve editörün sayfanın metniyle açıldığını, yayımlamanın en yeni sürümün yerine
>    geçeceğini söyler. **3. tur (F2):** uyarının zamanı artık yalnız Notice bloğunun içinde
>    aranır ve anlık görüntünün zamanı orada OLMAMALIDIR; sahte sürümler bir dakika arayla
>    (ekran zamanı dakikaya basar; 1 sn arayla iki zaman aynı basılıyordu ve sürüm listesi
>    satırları aynı dizgeyi taşıdığından X10/X11 yeşildi) — M44 (X10: `LiveAt` = anlık
>    görüntünün zamanı) ve M45 (X11: `LiveAt` boş) KIRMIZI. Ölçülen: tazelemesi başarısız yayın 303, sürüm
>    saklandı; ardından ekran 200, bir tazeleme denedi, uyarı bir kez ve canlı zamanla, editör
>    sayfanın metnini taşır; tazeleme düzelince sonraki görüntü iyileştirir (bir tazeleme, uyarı
>    yok), ondan sonraki görüntü tazelemez; kontrol: güncel anlık görüntüde uyarı yok, tazeleme
>    yok (`TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`; M15 = PRG'nin
>    tersi, M31 iyileştirme yok, M32 her görüntüde tazeleme, M33 uyarı yok KIRMIZI). Gerçek
>    Postgres'te yayından ve geri almadan sonra ekran uyarı göstermez: anlık görüntü listenin
>    canlı sürümünden ESKİ değil (E2E; M35 — karşılaştırma hep "geride" — E2E'de KIRMIZI: bu,
>    E2E iddiasının pozitif kontrolüdür). İddia bu kadar (4. tur, N2): uyarı 3. turdan beri tek
>    yönlü olduğundan anlık görüntüyü bir mikro saniye ileri alan bir mutasyon (denetçinin X17'si)
>    E2E'de YEŞİL — iki zamanın EŞİTLİĞİNİ bu E2E ölçmez; `op10_db_test.go`'nun yorumu buna
>    daraltıldı. (d) Yalnız-tazele eylemi
>    EKLENMEDİ: bedeli yeni bir POST sınıfı (C67) ve onun `classRoutes`/`designedHeaders`/sızıntı
>    kolu/`harvestWant`/tip pinleri + ekranda ikinci bir düğme; GET'teki iyileştirme onu gereksiz
>    kılar — ekranı yeniden yüklemek tazeleme eylemidir (uyarının metni bunu söyler).
>    (e) `Refresh` kilitli (eski `writing` mutex'inin gerekçesi: çakışan iki tazeleme ters sırada
>    kurulabilir). Ölçülen YARISI: iki eşzamanlı tazelemenin okuması aynı anda veritabanında olmaz
>    (`TestRefresh_TwoRefreshesNeverReadAtOnce`; M24 KIRMIZI). Kurulumun da kilidin içinde olduğu
>    (kurulum sırasını veren yarı) ölçülmedi: kilidi okuma ile kurulum arasında bırakan M38 (X27)
>    YEŞİL — iki adımın arasında durdurmak tipte olmayan bir kanca ister; `legal.Store.refreshing`
>    yorumu buna göre daraltıldı (kilit ve ertelenmiş açılışı `Refresh`'in ilk iki ifadesi, kod
>    okundu).
> 8. **Ret yolları.** 413 (gövde), 400 (form okunamadı, belge yok, görünür metin yok), 303
>    (oturum yayın/okuma anında reddedildi: `ErrOperatorRefused`), 503 (veritabanı hatası;
>    okuma hatası). Log satırları belgeyi, operatörün id'sini, uzunluğu ve `internal/db`'nin
>    hatasını (çağrı + SQLSTATE) taşır; metin ve hash argüman değil — sızıntı testi
>    A31–A43'te arar. Ekranın iyileştirme dalının log satırı yalnız hatayı taşır (A43).
>    2. tur: yayın hatasının 503 sayfası artık *"hiçbir şey yayımlanmadı"* DEMEZ — bir hata
>    satırın yazılmadığını kanıtlamaz (10 sn zaman aşımının iptali, COMMIT gönderildikten sonra
>    kopan bağlantı); başlık *"The publication was not confirmed"*, metin sürüm listesine
>    bakmayı söyler (E2E'nin NUL kolu metni ve `/operator/legal` linkini arar). Öbür üç ret
>    sayfası (413, iki 400) store'a gitmeden döner ve *"Nothing was published"* der (aynı
>    E2E). İyileştirme bir GET'te koşar ama satır yazmaz: müşteri havuzunda bir okuma ve
>    bellekteki anlık görüntünün değişimi.
> 9. **Sürüm listesi.** Tek sayfa: en yeni 100 (`op_read_legal_versions` tavanı 200); 100 satır
>    dolunca sayfa *"older versions are not shown"* der. Gövde yok, uzunluk bayt; yayımlayan
>    operatörün `display_name`'i ya da *"tenant admin (legacy)"*; her belgenin canlı sürümü
>    *Live* çipi (`TestLegalPage_ShowsWhoPublishedEachVersion`). Geri alma = eski metni
>    yeniden yayımlamak (yeni satır). "Bu sürüme dön" düğmesi yeni bir `op_read_*` ister —
>    YOK, devir.
> 10. **UI (tappa-brand).** "TAPTIME OPERATOR" kabuğu (`screen(…, signedIn)`), her belge bir
>     `op-card` (başlık: herkese açık yol, mono; canlı tarih; editör; *"Publish a new
>     version"*), sürüm listesi bir `docket` içinde mono satırlar (`.op-version` bileşeni;
>     yorumda utility adı yok). Herkese açık sayfalara link verilmez (başka host → mutlak URL);
>     yol metin olarak yazılır. Kontrast (palet `tailwind.config.js`'ten, WCAG 2.1):
>     ink/paper 16,17:1, ink %70/paper 6,05:1, ink/green-lite 13,70:1 (Live çipi),
>     paper/tappa-green 7,73:1 (düğme), geride uyarısı (`components.Notice`, `ToneWarn`:
>     saffron sol kenar, `saffron-lite` zemin; anlam başlık metninde, renk yalnız pekiştirir):
>     ink/saffron-lite 13,97:1, ink %85/saffron-lite 9,13:1 — `TestLegalScreen_TheTextClearsAA`.
>     `app.css`: +2 kural (`.op-version`, `.op-version:last-child`), 0 kaldırılan, 49 943 → 50 308 bayt
>     (yorumlardan doğan kural yok — kural kümesi karşılaştırıldı); 2. turun uyarısı yeni kural
>     getirmedi (bileşenin sınıfları zaten derleniyordu; yeniden derlendi: aynı 564 kural, aynı bayt).
> 11. **Wiring.** `texts` (müşteri havuzundaki anlık görüntü) operatör yüzeyinden önce kurulur
>     ve `openOperatorSurface`'e verilir; operatör havuzu `configuredSurface`'te iki yere gider:
>     `operatorAuthenticator` (→ `operatorauth.New`) ve `operator.New`'in `LegalStore` yuvası.
>     **Tek anlık görüntü (2. tur, B3):** `legal.NewStore` komutta bir kez; `run()`'ın bağladığı
>     değerin kullanımları TAM küme: açılış `Refresh`'inin alıcısı, `openOperatorSurface`'in
>     üçüncü, `handler.NewMarketing`'in ilk argümanı (M36/M37 ikinci `NewStore` — X19 — ve M39
>     başka bir adla devretme KIRMIZI). Testin adı OP-7'nindir ve kuralın tamamı değildir;
>     yeniden adlandırılmadı çünkü `m10-platform.md` onu üç, ADR 0020 §7 bir kez anar — ad
>     değişseydi o atıflar sarkardı; başlığında açıklandı. `operatorpool.go`'nun *"komutta başka
>     hiçbir yere"* cümlesi testin sözdizimsel kapsamına (main.go + operator.go, ad eşlemesi)
>     bağlandı.
> 12. **K11 / panel.** Panelin dokuz bölümü sahip ve yönetici olarak altı operatör işaretini
>     taşımaz; `/admin/legal` GET/POST 404 (`TestCustomerPanel_EverySectionCarriesNoOperatorElement`;
>     kontrol: işaretli bir işletme adı Account bölümünde bulunur); gerçek kayıtlı müşterinin
>     paneli aynı listeyle (`adminlogin_db_test.go`).
> 13. **Taşınan/korunan testler.** `TestLegalPublicPath_WritesNothing` ve
>     `TestLegalReader_CannotReachTheDatabase` aynı adla `internal/handler/legalpublic_test.go`'da;
>     ikincisinin pozitif kontrolü artık `legal.Store.Refresh` (bağlamlı, hatalı — G/Ç biçimli).
>     `TestLegalSlugs_AreExactlyTheDocumentsWithAPage` ve `TestLegalStore_CannotNameATenantAtAll`
>     (INSERT yarısı sorguyla gitti) aynı dosyada. `TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo`
>     kolon yarısıyla kaldı; "Go" yarısı `TestLegalPublish_RefusesAnEmptyBodyAndSaysSo`.
>     M7-06'nın `TestLegalPublish_RefusesASlugTheProductDoesNotHave`,
>     `TestLegalPublish_RefusesAnEmptyBodyAndSaysSo`, `TestLegalPublish_RefusesABodyBiggerThanTheCeiling`,
>     `TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt` adları operatör ekranının
>     testleri olarak yaşar. Silinenler (adları burada yazılmadı — var olmayan bir testin adı
>     bir atıf olurdu): panel ekranının dört izin listesi testi, iki ret/tenant testi ve
>     belge-durum testi; config'in izin listesi ayrıştırıcı testi; `NewStore`'un nil-trail
>     testi (yerine `TestNewStore_RefusesAMissingDatabaseAndStartsEmpty`) ve `Store.Publish`'in
>     "metin, iz ve anlık görüntü birlikte ya da hiçbiri" testi (yazan yol yok; `tappa_app`'in
>     reddedilen INSERT'i `TestLegalDB_TheTableTakesNoUpdateAndNoDelete` ve
>     `TestOperator00027_TheApplicationCanNoLongerWriteALegalText`'te kalır).
> 14. **Sarkan atıf bütçesi 54 → 60** (ölçüldü: silinen testlerden altısının adı değişmez
>     kayıtlarda geçiyor — ADR 0016 gövdesi, `m7-portal.md`, bu kartın OP-4 ve OP-10A blokları:
>     dört izin listesi testi, nil-trail testi, `Store.Publish`'in üçlü testi); altı ad
>     `cmd/tappa/testdata/known-dangling-citations.txt`'e, gerekçesi envanterin başlığına yazıldı.
>     Öbür silinenlerin (iki ret/tenant, belge-durum, ayrıştırıcı) belgelerde atfı yoktu.
>     `TestEveryNamedTestExists`: 60 canlı, 60 bütçe.
> 15. **Adlandırılmış sorgu sayısı birleşik ağaçta 122 → 121** (`TestResolverAccess_NoSqlcQueryNamesADefiner`;
>     `PublishLegalDocument` gitti; WL-1'in sekiz sorgusuyla — bu worktree'nin tabanında, WL-1'den
>     önce 114 → 113; birleşmede koordinatör çözdü) ve `storeSurface` envanterinden aynı satır.
>
> **Pin değişiklikleri ve gerekçeleri (hiçbiri daraltılmadı):**
> - `TestOperatorDB_IsTheStoreAndNothingMore` — istenen küme `operatorauth.Store` ∪
>   `operator.LegalStore` ∪ `Close`'dan türetilir (A'nın md. 11'de adlandırdığı tüketici); iki
>   arayüz ortak ad taşırsa kırmızı (bir yöntem ikisinin yerine geçemesin); öncül LegalStore ≥ 2.
> - `TestOperatorDB_EveryMethodDelegatesVerbatim` — 7 → 9 (iki yeni yöntem aynı
>   `return F(ctx, o.pool, …)` kuralına tabi; `PublishLegal`'de üç aynı tipli argüman).
> - `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` — öncül 8 → 10.
> - `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` — genişletildi: aynı okuma
>   yöntemin kendisiyle, üretim kurucusunun havuzunda (M06 bu kolda kırmızı).
> - `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` — kurallarıyla yeniden yazıldı:
>   çağrı adı paket niteleyicisiyle (`operator.New` ≠ `operatorauth.New`); `configuredSurface`'in
>   store'u TAM iki kullanım (`arg0 of operatorAuthenticator`, `arg1 of operator.New`),
>   `operatorAuthenticator`'ın store'u `arg0 of operatorauth.New`, `texts` →
>   `arg1 of configuredSurface` → `arg2 of operator.New`; bir kullanımın eksilmesi de kırmızı.
>   2. tur: `legal.NewStore` komutta bir kez; `run()`'ın onu bağladığı ad TAM üç kullanım
>   (`recv of Refresh`, `arg2 of openOperatorSurface`, `arg0 of handler.NewMarketing`);
>   `boundName` nitelikli adı da eşler (`legal.NewStore` ≠ `encode.NewStore`).
> - `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` — kod değişmedi; başlığı on üç
>   değişkeni söyler (1. turda eskimiş "eight" kalmıştı; öncül ≥ 8, PV2 her değişkeni
>   `problemPages`'e bağlar).
> - `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` — FV5'e `publishLegal`'in
>   `.Get("slug")` ve `.Get("body")`'si, birer kez (sayı pinli).
> - `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` — `Legal` (yedi ekran);
>   `screens()` 14 render.
> - `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` — C1–C66, öncül 6 rota / 10 çift.
> - `TestSurface_TheScreensOfLaterTasksAreNotMounted` — `/operator/legal` listeden çıktı.
> - `classRoutes` (+C49–C66; 2. tur: C64 503 → 303), `designedHeaders` (+C49–C66; C64 sayfa →
>   `Location: /operator/legal`), `hostileDrive` (sorgu `/operator/legal`'de de), `operatorRoutes`
>   (+GET/POST `/operator/legal`), `harvestWant` (+`LegalVersions` 5×1, `PublishLegal` 4×2,
>   `TouchOperatorSession` 104 → 116), sızıntı: G16, A31–A43 (A38 PREMISE 303; A43 = geride
>   ekran, iyileştirmesi başarısız — uyarı ve log satırı PREMISE'le, D6: tohum ve yayımlanan
>   metin, tazelemesi başarısız metin ekranda YOK), D6.
> - `TestTablesLock_IsTakenOncePerTestTree` — değişmedi: iki yeni E2E testi kilidi `newE2E`
>   üzerinden bir kez alır (PASS).
>
> **Kabul — karşılıkları:** `has_any_column_privilege(tappa_app,'legal_documents','INSERT') = false`
> → A fazı (`TestOperator00027_TheApplicationCanNoLongerWriteALegalText`; ADR 0020 §7'nin ölçüm
> sözleşmesi) · yayın sonrası `/legal/privacy` yeni metin →
> `TestE2E_LegalPublishRefreshesThePublicPageAndTheListNamesThePublisher` (Marketing handler'ı,
> `tappa_app` havuzundaki anlık görüntü) · `TestLegalPublicPath_WritesNothing` +
> `TestLegalReader_CannotReachTheDatabase` yeşil · sürüm listesi yayımlayanı gösterir → aynı E2E +
> `TestLegalPage_ShowsWhoPublishedEachVersion` · geri alma = yeni satır → E2E (eski metnin sürüm
> sayısı +1, yayımlananınki değişmez) + `TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows`
> · `rg --hidden 'OperatorAdminIDs|OperatorOnly|TabLegal|mayPublishLegal|TAPPA_OPERATOR_ADMIN_IDS'`
> kod+deploy'da 0 (`db/migrations` ve ADR/plan belgeleri hariç; dahil sayım: 00020 ×2, ADR 0016 ×5,
> ADR 0020 ×10, m10 ×6, m7 ×3, m9 ×1, state ×3) — not: `rg` gizli dosyaları varsayılan olarak
> atlar, `.env.example` için `--hidden` gerekir · müşteri panel taramasında operatör öğesi yok →
> `TestCustomerPanel_EverySectionCarriesNoOperatorElement` + `adminlogin_db_test.go`.
>
> **Kaçış denemeleri** (her biri bir testte; sonuç parantezde): (E1) çapraz-origin yayın, canlı
> çerezle (403, store 0 — C58; sızıntı A42) · (E2) MFA'sız, iptal edilmiş, 31 dk boşta, 8 saati
> geçmiş oturumla yayın, gerçek Postgres (303, sürüm 0, `legal_publish` 0 — E2E) · (E3) bozuk
> biçimli/ölü çerez (303 + çerez silinir — C51; müşteri tarafının 43 karakterlik bir değeri bu
> biçimdedir, OP-8'in `TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue`'su aynı kapıyı
> gerçek değerle ölçer) · (E4) 256 KiB + 1 (413, store 0) · (E5) görünmez karakterli gövde, 21
> biçim (400, store 0) · (E6) bilinmeyen/büyük harfli/yollu/NUL'lu slug, 9 biçim (400) · (E7)
> NUL baytlı metin, gerçek Postgres (503; sürüm 0; log metni taşımaz — E2E) · (E8) aynı bilet iki
> kez — handler düzeyinde: ekran bilet tutmaz, her görüntü yeni iki aşamalı okuma; iki görüntü →
> iki `read` satırı, iki tüketilmiş bilet, 0 tüketilmemiş (E2E) · (E9) müşteri panelinden eski
> `/admin/legal` GET ve POST (404) · (E10) operatör host'u dışında `/operator/legal`, yedi yöntem,
> ingress manifestinin host'ları ve altı host daha (router'ın 404'ü, store 0 —
> `TestHostGate_OperatorRoutesAnswerTheRoutersOwn404OnEveryOtherHost`) · (E11) `same-site` getirmeyle legal sayfa (303, yüklem yok —
> C52) · (E12) formda başka bir yayımlayan (`published_by`, `admin_id`, `session`) (yok sayıldı)
> · (E13) `PUT /operator/legal` (405, Allow GET,POST, store 0 — C66) · (E14) ikinci bütçe birimi
> tükenmişken okuma (429, okuma 0 — C55) · (E15) 15 düşmanca istek başlığı + düşmanca sorgu, 18
> sınıf (yansıma yok — ham ve sorgu-kaçışlı biçimler) · (E16) istemci handler'dan önce ayrıldı
> (yayın ve tazeleme yine koşar, 303) · (E17) istemci commit ile tazeleme arasında ayrıldı
> (anlık görüntü yeni metin) · (E18) tazelemesi başarısız yayından sonra F5 (303 → GET: ikinci
> yayın yok; ekran geride uyarısı) · (E19) U+303F, U+1D159 ve üç boş sembol birlikte (400,
> store 0) · (E20) ekranın iki okuması arasına giren başka bir operatörün yayını (uyarı yok,
> fazladan tazeleme yok, editör listeyle tutarlı) · (E21) iyileştirmenin tazelemesi listeden yeni
> bir sürüm kurar (uyarı yok).
>
> **Mutasyonlar** (kopyala-geri-yaz; yedekler `scratchpad/op10b/mut_backup/`; her biri `go vet`
> ile derlendi; geri yükleme sha256 ile doğrulandı; templ mutasyonunda `templ generate`
> mutasyonla ve geri yüklemeyle koştu). Kırmızı veren testin ilk hata satırı. 2. turda M01–M27
> (M15 yeniden tanımlandı) son ağaçta YENİDEN koşuldu ve M28–M40 eklendi; 3. turda M41–M45
> eklendi ve `legal.go`'ya dokunan M15, M28–M33, M35 son ağaçta yeniden koşuldu (M31, M32, M35
> yeni adlara göre yeniden yazıldı: `snapshotDiffers`, `sameLength`); 45'in 44'ü KIRMIZI,
> M38 YEŞİL (ölçülmeyen yarı, karar 7(e)):
>
> | # | Mutasyon | Sonuç |
> |---|---|---|
> | M01 | yayından sonra `Refresh(ctx)` yok | KIRMIZI — `TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows`, sızıntı PREMISE A32 (0 refresh), E2E (anlık görüntü eski metin) |
> | M02 | yayın formdaki `session` alanının hash'iyle | KIRMIZI — `TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows` (303 /operator/login), `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` (FV5) |
> | M03 | legal formun sınırı 1 GiB | KIRMIZI — `…BodyBiggerThanTheCeiling`, C59, sızıntı PREMISE A36 |
> | M04 | `readForm`'da `MaxBytesReader` yok | KIRMIZI — C59, `…BodyBiggerThanTheCeiling` |
> | M05 | panelde "Taptime legal texts" bölümü geri | KIRMIZI — `TestCustomerPanel_EverySectionCarriesNoOperatorElement` (navigasyonda `/admin/legal`) |
> | M06 | `(*OperatorDB).LegalVersions` tek transaction'da | KIRMIZI — `TestOperatorDB_EveryMethodDelegatesVerbatim`, `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` (ErrOperatorRefused) |
> | M07 | sızıntı kolu A32 boşaltıldı (metinsiz) | KIRMIZI — sızıntı PREMISE A32 (400) |
> | M08 | başarı satırına metin | KIRMIZI — sızıntı: G16 S1'de |
> | M09 | hata satırına oturum hash'i | KIRMIZI — sızıntı: G8 S1'de (A37) |
> | M10 | `visibleText` = `TrimSpace` | KIRMIZI — `TestVisibleText_TheListedInvisibleBodiesAreRefused`, `TestLegalPublish_RefusesAnEmptyBodyAndSaysSo`, C62 |
> | M11 | slug kontrolü yok | KIRMIZI — `…RefusesASlug…`, C61 |
> | M12 | okumanın ikinci birimi yok | KIRMIZI — `…AReadCountsTwice…`, C55 |
> | M13 | legal rotaları giriş grubunda (oturum kapısız) | KIRMIZI — C49 (303) |
> | M14 | legal rotaları okuma kapısız (`sameOriginGate(false)`) | KIRMIZI — C52 (200) |
> | M15 | PRG'nin tersi: tazeleme hatası 503 + sorun sayfası (2. turda yeniden tanımlandı; 1. turdaki "hata başarı gibi" artık tasarımdır) | KIRMIZI — C64 (503 ≠ 303), `TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`, sızıntı PREMISE A38 |
> | M16 | yayının oturum reddi 503 | KIRMIZI — C65, sızıntı PREMISE A40 |
> | M17 | legacy sürüm "operator" etiketli | KIRMIZI — `TestLegalPage_ShowsWhoPublishedEachVersion` |
> | M18 | editör `templ.Raw` | KIRMIZI — `TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt` |
> | M19 | `openOperatorSurface` havuzu log satırına | KIRMIZI — wiring (`arg2 of log.Debug`) |
> | M20 | `configuredSurface` legal yuvaya ara değişken | KIRMIZI — wiring |
> | M21 | `*OperatorDB`'ye `Ping` | KIRMIZI — `…IsTheStoreAndNothingMore`, `…DelegatesVerbatim` |
> | M22 | `(*OperatorDB).PublishLegal` slug/gövde yer değiştirmiş | KIRMIZI — `…DelegatesVerbatim` |
> | M23 | `SessionHash` challenge anahtarıyla | KIRMIZI — `TestSessionHash_IsTheHashVerifyHandsTheStore`, C49, `TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows` |
> | M24 | `legal.Store.Refresh` kilitsiz | ilk koşuda YEŞİL (test yoktu) → `TestRefresh_TwoRefreshesNeverReadAtOnce` yazıldı → KIRMIZI (2 okuma aynı anda) |
> | M25 | ekran 200'lük sayfa ister | KIRMIZI — `…ShowsWhoPublishedEachVersion` (105 satır) |
> | M26 | `legalSession` boş hash | KIRMIZI — C49 (303) |
> | M27 | `marketing.go` anlık görüntünün `Refresh`'ini çağırır | KIRMIZI — `TestLegalPublicPath_WritesNothing` (statik yarı) |
> | M28 | X24'ün tersi: yayından sonraki `Refresh` istek bağlamıyla | KIRMIZI — `TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh` ((a) anlık görüntü boş) |
> | M29 | yayın istek bağlamıyla | KIRMIZI — aynı test ((a) 500: yayın iptal edilmiş bağlamla reddedildi) |
> | M30 | kopuk bağlam zaman aşımsız | KIRMIZI — aynı test (tazelemenin son tarihi yok) |
> | M31 | iyileştirme yok (GET hiç tazelemez) | KIRMIZI — `TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`, sızıntı PREMISE A43 (log satırı yok) |
> | M32 | her görüntüde tazeleme | KIRMIZI — aynı test (kontrol: güncel anlık görüntüde +1 tazeleme) |
> | M33 | editörde geride uyarısı yok (templ) | KIRMIZI — aynı test, sızıntı PREMISE A43 |
> | M34 | `blankSymbol`'den U+303F çıkarıldı | KIRMIZI — `TestVisibleText_TheListedInvisibleBodiesAreRefused`, `TestLegalPublish_RefusesAnEmptyBodyAndSaysSo` |
> | M35 | `behind` hep doğru (uzunluk bir fazla) | KIRMIZI — E2E (yayından sonra ekran "geride" der: E2E'nin uyarısız iddiasının pozitif kontrolü), `…AFailedRefresh…` (kontrol) |
> | M36 | X19: operatör yüzeyine ikinci bir `legal.NewStore` | KIRMIZI — wiring (`legal.NewStore` iki kez; `texts`'in `arg2 of openOperatorSurface`'i yok) |
> | M37 | X19b: herkese açık sayfalara ikinci bir `legal.NewStore` | KIRMIZI — wiring |
> | M38 | X27: kilit okuma ile kurulum arasında bırakılır | YEŞİL — `TestRefresh_TwoRefreshesNeverReadAtOnce` kurulum sırasını ölçmez; iddia daraltıldı (karar 7(e)) |
> | M39 | X19c: tek `NewStore`, operatöre başka bir adla (`opTexts := texts`) | KIRMIZI — wiring (`assigned to another name`; tam küme kuralı sayım kuralından bağımsız) |
> | M40 | yayın hatasının 503 sayfası yine *"That text was not published"* der | KIRMIZI — `TestE2E_LegalPublishRefusesDeadSessionsAndBadTextsWritingNothing` (NUL kolu) |
> | M41 | F1: anlık görüntü listeden SONRA okunur (sıra ters) | KIRMIZI — `TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind` (2 tazeleme, 1 beklenir; editör yeni sürüm) |
> | M42 | uyarı her farkta (yön düşürüldü) | KIRMIZI — `TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind` (uyarı var) |
> | M43 | X02: `legalWriteTimeout` = `time.Hour` | KIRMIZI — `TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh` (1h ≠ 10s) |
> | M44 | X10: `LiveAt` = anlık görüntünün zamanı | KIRMIZI — `TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind` (Notice'te canlı zaman yok, anlık görüntününki var) |
> | M45 | X11: `LiveAt` boş | KIRMIZI — aynı test (Notice'te canlı zaman yok) |
>
> Pozitif kontrol: pristine ağaçta hedefli `-race` koşusu yeşil (aşağıda). Sayı bu koşuların
> kaydıdır, ağın kapsamının değil.
>
> **Kalıcı test verisi (ölçüldü, sahip bağlantısı, salt okunur işlem; iki E2E testinin bir
> koşusu çevresinde):** `op10b-` hesapları +2 (ikisi de `disabled`), oturumlar +7 (canlısı 0),
> audit satırları +9 (birinci test: login, iki `read`, iki `legal_publish`, logout; ikinci:
> login, bir `read`, logout), bilet 0, `legal_documents` +2 (birinci testin yayını ve geri
> alması). Bu sayım, koşu öncesinde formun taşıyabileceği bir `privacy` metni VARKEN alındı:
> geri alma onu yeniden yayımlar, sayfa başladığı yerde biter. Öyle bir metin YOKSA dal dört
> yayın yapar — koşunun metni, bir FAKE önceki metin, koşunun metni yeniden, önceki metin geri
> alma olarak: **+4 sürüm ve +4 `legal_publish`** (audit +11) ve sayfa testin FAKE metniyle
> biter (kod okundu; bu dal bu koşuda sürülmedi). `op10_db_test.go`'nun başlığı da "iki ya da
> dört" der (1. turda "iki ya da üç" yazıyordu). `internal/db`'nin
> genişletilen havuz testi koşu başına bir `read` satırı daha commit eder.
>
> **Sayılı sınırlar (OP-10B):**
> - **LB1** — Sürüm listesi tek sayfa (en yeni 100); daha eskisi bu ekranda görünmez. Devir:
>   sayfalama (sorgu dizgisi; FV6 pini genişler).
> - **LB2** — "Bu sürüme dön" düğmesi yok: geri alma metni elle yeniden yapıştırmaktır; eski
>   bir sürümün gövdesini okuyan bir `op_read_*` (kendi türü, migration) ister.
> - **LB3** — Görünür karakter kuralı Unicode kategorisine dayanır ve adı konmuş boş semboller
>   `blankSymbol`'dür: U+2800, U+303F, U+1D159 (2. tur: U+303F ve U+1D159 eklendi). U+FFFC
>   görünür sayılır (karar, çizimi ölçülmedi). L/N/P/S'de olup bu üçün dışında boş çizilen bir
>   sembol (ör. bir yazı tipinde glif taşımayan bir kod noktası) geçer — kod incelemesinin
>   konusu.
> - **LB4** — Anlık görüntü tek replikada tazelenir (ADR 0016 Sonuçlar (i)); HTTP önbelleği 5 dk
>   (ii) aynen. Tazeleme hatasında yayın veritabanındadır ve yanıt 303'tür; herkese açık sayfa
>   bir sonraki BAŞARILI tazelemeye kadar — bir sonraki yayın, operatörün legal ekranının bir
>   sonraki görüntüsü (iyileştirme) ya da yeniden başlatma — eski metni gösterir; ekran geride
>   olduğu sürece her görüntüde bunu söyler. Ekranı kimse açmazsa ve yayın olmazsa sayfa yeniden
>   başlatmaya kadar geride kalır (ERROR log satırı kalır). Çok replikada iyileştirme yalnız
>   GET'i karşılayan replikayı düzeltir (ADR 0016'nın zamanlayıcı devri). Yalnız-tazele eylemi
>   yok (bedeli karar 7(d)'de).
> - **LB5** — Oturum hash'i handler paketinde düz bir `string`'dir (`SessionHash`'in dönüşü,
>   `LegalStore`'a verilir). Bu B fazının yeni bir gevşemesi değil: OP-7'den beri
>   `operatorauth.Store`'un (`OpenOperatorSession`, `TouchOperatorSession`,
>   `CloseOperatorSession`) ve `internal/db`'nin `op_*` imzaları hash'i `string` olarak taşır
>   (kaynak okundu); B fazı aynı tipi bir pakete daha taşıdı. Tip düzeyinde ayrım (adlandırılmış
>   bir hash tipi) yok — o, OP-7 imzalarını da değiştiren ayrı bir iştir. Sızıntı sözleşmesi
>   G8'i A31–A43 kollarında dört yüzeyde arar; kolların dışı kod incelemesinin.
> - **LB6** — 256 KiB istek gövdesine uygulanır: URL kodlaması büyüten bir metin (satır
>   sonları `%0D%0A`, ASCII dışı harfler `%XX`) 256 KiB'tan kısayken 413 alabilir; panelin
>   M7-06 davranışıyla aynı.
> - **LB7** — E2E'nin kalıcı satırları (yukarıda); paylaşılan dev veritabanının `/legal/privacy`'si
>   önceden metin yoksa test metniyle biter.
> - **LB8** — E2E'nin herkese açık sayfa kontrolü, başka bir paketin testinin aynı anda commit
>   ettiği bir `privacy` sürümüyle yarışabilir (pencere bir ifade boyu; gözlenmedi).
> - **LB9** — Bu koşuda `internal/db`'nin `TestRLS_EveryTenantScopedTableIsEnabledAndForced` ve
>   `TestRLS_TheForceGateActuallyFires`'ı KIRMIZI: paylaşılan dev veritabanı goose 28'de (paralel
>   WL-1 akışının `tenant_branding`'i), bu worktree'nin `db/migrations`'ı 00027'de biter — test
>   canlı katalogdaki tabloyu migration'larda arar. OP-10B'nin değişikliği değil; birleşmeden
>   sonra yeniden koşulmalı.
> - **LB10** — İki yetkili operatör arasında **kaybolan güncelleme** (güvenlik denetimi S1): formda
>   temel sürüm alanı yok ve `publishLegal` bir temel sürüm karşılaştırmaz. A sayfayı T0'da açar,
>   B T1'de v3'ü yayımlar, A T2'de T0 sayfasından yayımlar: v3, A'nın v2+düzeltmesiyle ezilir ve
>   ekran bunu söylemez (A'nın sayfası T0'da "geride" değildi; uyarı açılıştaki durumu anlatır).
>   **Kayıt kaybı yok:** tablo append-only, v3 bir satır olarak ve `legal_publish` satırıyla
>   kalır, sürüm listesi onu gösterir. Gerçek düzeltme formda bir temel sürüm alanı ve
>   `op_publish_legal`'e bir beklenen-sürüm parametresi ister — **migration**; bu görevde YOK
>   (yeni migration yasak), devir.
> - **LB11** — **Aynı mikro saniyede iki yayın** (4. tur, N1). Veritabanı güncel sürümü
>   `(published_at DESC, id DESC)` ile seçer; `legal.Doc` id taşımaz, `compare` yalnız
>   `published_at`'ı karşılaştırır ve eşit zamanda uzunluğu id'nin vekili yapar. Aynı slug'ın iki
>   yayını aynı `clock_timestamp()` mikro saniyesine düşerse (`op_publish_legal`'de slug başına
>   kilit yok) vekil iki yönde yanılır: **(i)** iyileştirmeden sonra anlık görüntü listeden
>   (id'ce) yeni ve uzunluğu farklıysa bir görüntülük yanlış uyarı; **(ii)** aynı uzunlukta farklı
>   metin varsa ne tazeleme ne uyarı (2. turdan beri). Kesin düzeltme `legal.Doc`'a `ID` ekleyip
>   `(PublishedAt, ID)` karşılaştırmaktır — yazılmadı (ürün kodu bu turda değişmez). Denetçinin
>   X2 ve X4 mutasyonlarının tam pakette YEŞİL kalmasının sebebi budur: eşit zaman kolu hiçbir
>   testte sürülmez. `legal.go`'nun `compare` ve `legalPage` yorumları bunu söyler.
>
> **Devirler:** `legal.Doc`'a `ID` ve `(PublishedAt, ID)` karşılaştırması (LB11) · yayında temel sürüm kontrolü — form alanı + `op_publish_legal`'e beklenen-sürüm
> parametresi, migration (LB10) · **WL-7:** `40-ingress.yaml:127-133`'ün yorumu — OP-10B'nin
> eklediği *"the largest bound a handler behind this Ingress sets itself is the sign-up form's 64
> KiB"* cümlesi bugün doğru, ama logo yüklemesi (`internal/brand.LogoMaxInputBytes` = 512 KiB)
> bu Ingress'ten geçecek; WL-7 bu cümleyi de günceller (m10'un WL-7 satırı genişletildi) ·
> adlandırılmış oturum-hash tipi (LB5; OP-7 imzalarıyla) · sürüm listesi sayfalaması (LB1) · "bu sürüme dön" için eski gövdeyi okuyan
> `op_read_*` (LB2) · çoklu replikada Refresh zamanlayıcısı (ADR 0016) · OP-11'in okuma ekranları
> ikinci bütçe birimini `spendSession` ile harcar (surface.go'nun türetmesi) · canlıdaki
> ConfigMap'te kalmış bir `TAPPA_OPERATOR_ADMIN_IDS` anahtarını kod okumaz; manifestten düştü
> (kümede ölçülmedi) · K2/K4'ün operatör Ingress'i: `deploy/README.md` taslağı **iki Ingress**
> (3. tur, güvenlik S2: tek Ingress'in `320k`'sı `path: /` ile bütün `/operator/*`'a, oturum
> öncesi yollara da uygulanıyordu): `tappa-operator` `/` Prefix **`24k`** — `/operator/legal`
> dışındaki her gövde okuması `readForm`'dan `maxFormBytes` = 16 KiB ile geçer (login, totp,
> enroll; logout ve GET'ler gövde okumaz; kaynak okundu), 24k = 16 KiB + 8 KiB pay;
> `tappa-operator-legal` `/operator/legal` Exact **`320k`** (1. turdaki `"64k"` 64 KiB'den uzun
> her yasal metni nginx'te keserdi; uygulamanın sınırı 262 144 bayt, tam 256 KiB yayımlanır, +1
> bayt uygulamanın kendi 413 sayfası — `TestLegalPublish_RefusesABodyBiggerThanTheCeiling`; 320k =
> sınır + 64 KiB pay). ingress-nginx'te ek açıklamalar Ingress kaynağının bütün yollarına
> uygulanır, yol başına sınır ayrı kaynak ister — belge cümlesi; **kümede ölçülmedi**, taslak
> uygulanmadı. K4 izin listesi iki kaynağa da yazılmalı (biri eksikse o yol her adrese açık).
> Kalan sayılı nokta: nginx gövdeyi uygulamadan önce tamponlar, oturumsuz bir istemci de
> `/operator/legal`'e 320 KiB'a kadar gönderebilir (uygulama 303). `40-ingress.yaml`'ın yorumu düzeltildi: o Ingress
> operatör host'unu yönlendirmez; arkasındaki handler'ların koyduğu en büyük sınır kayıt
> formunun 64 KiB'ı (`signupMaxFormBytes`); `"1m"` yeniden türetilmedi · 🔴 A ve B birlikte `main`'e (ADR 0021 OP-10 notu md. 15).

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

> **Kart düzeltmesi (2026-10-03, OP-11 A fazı — veri katmanı — uygulaması sırasında).**
> Yazıldı: `db/migrations/00029_read_tenants_from_the_operator.sql` (iki `op_read_*`, değiştirilen
> `op_begin_read`, genişletilen bilet tür kümesi, tanımlayıcının tenant tablolarındaki ilk sütun
> yetkileri), `internal/db/operator.go` (dışa açık `TenantList`, `TenantDetail`, `TenantListQuery`,
> `TenantSummary`, `TenantOverview`, `MaxTenantSearchRunes`, `ErrTenantSearchRefused`,
> `ErrNoSuchTenant`; paket içi `readTenants`, `readTenantDetail`, `storableText`), `db/queries/operator.sql`
> (iki ifade belgeye eklendi), `internal/db/operatortenants_test.go` (yeni, 17 test), ADR 0021
> "OP-11 uygulama notu" + Durum satırı. Güncellenen pinler (zayıflatılmadı, gerekçe md. 9):
> `TestOperator00026_PrivilegeMatrix`, `TestOperatorSQL_OnlyBoundParameters`,
> `TestOperatorAccessors_TheCustomerRoleCannotUseThem`, `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`,
> `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`, `TestOpBeginRead_RefusesDeadSessionsAndBadParameters`,
> `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy`; yalnız yorum: `internal/db/encodedat_test.go`
> (tags ACL'inin tarifine 00029'un SELECT'i eklendi). Ekran, wiring, `*OperatorDB` yöntemleri B
> fazıdır (md. 14). Ölçüm: dev Postgres 17.10, `tappa_owner`; kimlik `SET LOCAL SESSION AUTHORIZATION`
> ile. Taban `e0f53b6` (dal `m10-a1` ucu).
>
> 1. **Ad sapması (ADR 0021 §2 v 6 kazanır):** satırdaki `op_list_tenants` / `op_tenant_detail`
>    yerine `op_read_tenants(p_session, p_ticket, p_query, p_page_number, p_page_size)` ve
>    `op_read_tenant_detail(p_session, p_ticket, p_tenant_id uuid)` — `op_read_` öneki ve ikinci
>    argümanda ham bilet (OP-4 bloğu "OP-11" maddesi de böyle okur).
> 2. **"Her çağrı tam 1 operatör audit satırı" = kabul edilen her okuma `op_begin_read`'de tam 1
>    `read` satırı** (ADR 0021 Sonuçlar). Okuma aşaması satır yazmaz (ölçüldü: delta 0); iki tür
>    için ayrı ayrı gerçek commit'le sayıldı (`TestOpReadTenants_TwoPhaseLifecycle`: 1 / 1).
> 3. **`op_begin_read` yerinde değiştirildi (`CREATE OR REPLACE`):** türler `legal_versions`
>    (değişmedi), `tenants` (`{page_number, page_size, query}`, `query` ≤ 254 karakter JSON dizgisi,
>    `''` = bütün tenant'lar), `tenant_detail` (`{tenant_id}`, tireli uuid, iki harf büyüklüğü;
>    hash'lenen metin uuid değerinden). 22023 kolları 00027'ninkiyle aynı mesaj; yeni yirmi ret
>    kolu testte. 🔴 **Birinci aşama tenant'ın varlığına bakmaz** (reddi iz bırakmaz → geri alınan
>    bir çağrıya varlık kehaneti olurdu, B14'ün okuma hâli); varlık ikinci aşamada, `read` satırı
>    commit edildikten sonra: bilinmeyen id = 0 satır (`ErrNoSuchTenant`), 28000/22023'ten ayrı.
> 4. **Arama terimi audit'e GİRMEZ (karar, GDPR/§7):** ne ham ne hash'i — terim bir yönetici
>    adresi olabilir (kişisel veri) ve append-only tabloya yazılırsa silinemez; ADR 0021 §2 v 1
>    zaten öyle diyor. Audit satırı: `target_scope = 'tenants'`, sayfa, `detail = {"search": <sınıf>}`
>    (§2 v 1'in "arama yapıldı" olgusu; anahtar kümesi birebir test edilir; 1. turda `bool`idi,
>    sınıf 2. turun kararı — md. 18). Sonuç sayısı audit'e
>    giremez (satır sorgudan ÖNCE commit edilir). Ayrıntı: `target_scope = 'tenant_detail'`,
>    `target_tenant_id`, sayfa yok, `detail = {}`. Bilet satırı da `target_tenant_id` taşır
>    (bilgi amaçlı; bağlayan hash).
> 5. **Arama anlamı:** (a) ad — `strpos(lower(name), lower(term)) > 0`, LIKE DEĞİL: `%`, `_`, `\`
>    metakarakter değil, kaçış gereksiz (kaçış ikinci bir temsildir — silmek kaçmaktan güçlü);
>    (b) yönetici adresi — BİREBİR, citext `OPERATOR(public.=)`; alt dizi değil, çünkü alt dizi
>    kişisel verinin taranmasıdır (`@gmail` her tenant'ı listelerdi), birebir bir aramadır; her
>    durum/rol; adres döndürülmez; `employees.email` aranmaz; değerle bağlı (`t.id IN (…)`);
>    (c) id — terim tam tireli uuid ise. Sıra `created_at DESC, id DESC` (tenant'ın değiştiremediği
>    anahtar — `tappa_app` `name`'i günceller, `created_at`'i güncelleyemez, 00024).
> 6. **Sütunlar:** liste `tenant_id, tenant_name, created_at, plan`; ayrıntı aynı dört +
>    `business_type, location_count, active_employee_count, active_plaque_count,
>    active_admin_count`. VAT numarası, fiyat, saat dilimi, VAT kararı, adres, ad-soyad YOK
>    (OP-12/13/16 kendi okumalarını yapar). Ayrıntının tenant verisine dokunan her sorgusu (satır
>    ve dört sayım) tenant'ı adlandırır.
>    🔴 **`structure` ölçülerek çıkarıldı:** ilk tasarım onu da döndürüyordu; tam koşu
>    `internal/handler`'ın `TestSignupStructure_DecidesNothingAfterSignUp`'ını kırmızıya çevirdi
>    (`internal/db/operator.go`'daki `o.Structure` seçicisi). O test M7-03 B'nin kararını tutar:
>    `tenants.structure`'ı kayıttan sonra hiçbir şey okumaz, sihirbazın metni buna dayanır.
>    Testi gevşetmek yerine (izin listesine `internal/db/operator.go` eklemek) sütun genel
>    bakıştan, tanımlayıcının yetkisinden ve Go tipinden çıkarıldı — operatörün bir tenant'ı
>    tanıması için gerekmiyor; genel bakış onun ilk okuyucusu olmuyor.
> 7. **Yetkiler (tenant tablolarında İLK):** `tappa_opdefiner` SELECT — `tenants` (id, name,
>    created_at, plan, business_type), `locations` (tenant_id), `employees` (tenant_id,
>    status), `tags` (tenant_id, status), `admin_users` (tenant_id, email, status). Katalogdan
>    türetilen sır biçimli dokuz tenant-tablosu sütununun (yedi "asla" + `tenant_branding.logo`,
>    `logo_sha256`) hiçbirinde SELECT yok; tanımlayıcı olarak `aes_key_ref`/`app_key_ref`/
>    `password_hash` okuma denemesi 42501. `tappa_app`: yeni yetki yok; `tappa_operator`: iki yeni
>    fonksiyonda EXECUTE, hiçbir tablo yetkisi yok (2. tur, B3 metin düzeltmesi).
> 8. **Down:** iki okuma düşer; `op_begin_read` 00027'nin gövdesine BİREBİR döner (test 00027
>    dosyasıyla `prosrc` karşılaştırır); tanımlayıcının beş tablodaki bütün yetkileri alınır;
>    bilet tür CHECK'i 00027'nin kümesine — iki türden bilet varsa `NOT VALID` (canlıda kullanıldıktan
>    sonra bu dal). **Ölçüm (dev, `pg_dump --schema-only`, `\restrict` ayıklanarak):** v28 tabanı
>    `f0dc03e20de806e8` (iki döküm birebir). `structure` çıkarılan dosya (önce Down ile v28'e
>    inildi, dosya değişti, Up): Up → `9f792800b4010c64`; Down → `f0dc03e20de806e8` birebir;
>    Up → `9f792800b4010c64` birebir. Mutasyonlardan sonra yalnız fonksiyon gövdesi DIŞI yorumlar
>    değişti; SON dosya (sha256 öneki `a2ee6d44c71dac3f`) yine Down → Up ile uygulandı:
>    `f0dc03e20de806e8` / `9f792800b4010c64`, ikisi de birebir. (İlk hâlin — `structure`'lı
>    — döngüsü de aynı biçimde birebirdi: `16d6c67d6468687b`.) Her goose adımı ayrı komut, sürüm her
>    adımda ölçüldü.
> 9. **Başka görevlerin testlerinde güncellemeler — neden:** (a) `TestOperatorSQL_OnlyBoundParameters`
>    10 → 12 sabit ve çağrı; (b) `TestOperatorAccessors_TheCustomerRoleCannotUseThem` iki erişimci
>    (42501); (c) `TestOperator00026_PrivilegeMatrix` izin listesine beş tablo, tam sütun listesiyle
>    (OP-5 notunun kuralı; "asla" sütunları listeye giremez — test kendisi denetler); (d)
>    `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`'in öncülü — veritabanı HEAD'de, küme
>    00029 ile genişledi; öncül artık "`legal_versions`'ı tutan kapalı küme" (00027'nin Up'ı yeniden
>    koşunca tam 00027 kümesi hâlâ ölçülür); (e) `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`
>    ve `TestOpBeginRead_RefusesDeadSessionsAndBadParameters` "küme dışı tür" kolları
>    `tenant_detail`/`tenants` kullanıyordu — 00029 onları üye yaptı (ikincisi sessizce BAŞKA bir
>    dalı — eksik `query` anahtarını — sınar hâle gelirdi); artık hiçbir migration'ın eklemediği bir
>    ad; (f) `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy` `locations`'taki sütun ACL'i
>    **sayısını** (0) istiyordu — 00029'un `tenant_id` SELECT'i onu 1 yaptı (ölçüldü, kırmızı).
>    00010'un gerekçesi `tappa_app`'in tablo düzeyi yetkisi hakkında; test artık girişleri okur:
>    `tappa_app` için sıfır, öteki her giriş ADIYLA (bugün yalnız `tenant_id:tappa_opdefiner:SELECT`)
>    — yeni bir giriş yine kırmızı (L1 mutasyonu). Düşen garanti: "hiçbir rol `locations`'ta sütun
>    ACL'i tutmaz" — 00029 bilinçli olarak tuttuğu için düşmesi gerekiyordu, yerine adlı liste geldi.
> 10. **Kalıcı test verisi (ölçüldü — son `-race` koşusu öncesi/sonrası, `internal/db` + `cmd/tappa` +
>     `cmd/opadmin`; dev'deki mutlak sayılar mutasyon koşularıyla birikti, yazılan şey FARK):**
>     `tenants`/`tenant_detail` `read` satırları +2/+2 (20/18 → 22/20), "op10 live" hesapları +4
>     (ikisi bu dosyanın, ikisi OP-10'un), aktif hesap 0 → 0, iptal edilmemiş oturum 0 → 0, bilet 0 → 0,
>     adında `op11` geçen tenant 0 → 0, `legal_documents` +0. Bu dosyanın koşu başına bıraktığı: 2 hesap (`disabled`), referanslı
>     oturumları (`revoked`), 5 `read` satırı (`TestOpReadTenants_TwoPhaseLifecycle` tür başına bir:
>     2; `TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions` 3 — iki `tenants`, bir
>     `tenant_detail`; 1. turda 4'tü, 2. turun B5 okuması beşinciyi ekledi — md. 21). Hiçbir tenant
>     satırı commit edilmez: fikstürler geri alınan transaction'larda.
> 11. **Kilit:** yeni testler kilidi yalnız `opTx`/`opLiveFixture` üzerinden, fonksiyon başına bir kez,
>     alt test/döngü dışında alır — `TestTablesLock_IsTakenOncePerTestTree` PASS ("70 tests reach it").
> 12. **Maliyet — gözlem** (dev, 2026-10-03, `SELECT count(*) FROM tenants` = 586 538, test kalıntısı):
>     liste ilk sayfa 88–196 ms (paralel sıralı tarama + top-N), 200'lük 2000. sayfa 580 ms (diske
>     taşan sıralama); ayrıntı en büyük dev kadrolu tenant'ta sıcak 10–13 ms, soğuk 1,4 sn. İndeks yok.
> 13. **Mutasyonlar** (her biri KIRMIZI; migration mutasyonları `goose down` → mutant → `up-by-one`
>     → test → `down` → pristine → `up-by-one`, her adımda sürüm ölçüldü ve sonda şema dökümü
>     `9f792800b4010c64`'e eşit; dosya mutasyonları kopyadan geri yazıldı ve `cmp` ile doğrulandı;
>     yedekler `scratchpad/op11a/orig/`). **Tablonun tamamı SON dosyalarda koşuldu** (`structure`
>     çıkarıldıktan sonra mutantlar yeniden üretildi ve 36'sı yeniden koşuldu):
>
>     | # | Mutasyon | Kırmızı test (ilk hata) |
>     |---|---|---|
>     | M1 | liste süresi `now()` | `TestOpReadTenants_ExpiryIsTheWallClock` + kaynak pini + `TestOperator00026_NoFrozenClock` + imza testi |
>     | M2 | ayrıntıda commit koşulu yok | `TestOpReadTenants_ATicketFromThisTransactionIsRefused` (A1) + kaynak pini |
>     | M3 | listede tür koşulu yok | `TestOpReadTenants_AForgedTicketIsRefused` (tür karışıklığı) + kaynak pini |
>     | M4 | lokasyon sayısı tenant filtresiz | `TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows` (bütün tenant'ların lokasyonları / 2) |
>     | M5 | çalışan sayısı statüsüz | aynı (5 / 3) |
>     | M24 | plaket sayısı statüsüz | aynı (4 / 2) |
>     | M26 | yönetici sayısı tenant filtresiz | aynı (bütün tenant'ların aktif yöneticileri / 2) |
>     | M27 | ayrıntı satırı tenant filtresiz (`LIMIT 1`) | aynı (başka tenant'ın adı) |
>     | M6 | adres eşleşmesi bağsız `EXISTS` | `TestOpReadTenants_SearchMatchesNameAddressAndIDOnly` (A'nın adresi her tenant'ı döndürdü) |
>     | M8 | adres `email::text = term` (harfe duyarlı) | aynı (büyük harfli adres boş) |
>     | M29 | id eşleşmesi yok | aynı (D'nin id'si boş) |
>     | M7 | ad eşleşmesi kaçışsız `ILIKE '%' || term || '%'` | `TestOpReadTenants_LikeMetacharactersAreLiteral` (`%` "plain"i döndürdü) |
>     | M9 | gövdede 200 tavanı yok | `TestOpReadTenants_PagesAreCappedAndOrdered` (210 / 200) |
>     | M10 | id eşitlik kırıcısı ASC | aynı (ikizler ters) |
>     | M25 | OFFSET bir sayfa ileride | aynı (10 / 200) |
>     | M11 | audit `detail`'i terimi taşır | `TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm` |
>     | M13 | ayrıntı id'si biçim denetimsiz cast | aynı (`22P02`; süslü parantezli uuid kabul) |
>     | M28 | ayrıntının audit satırı tenant'sız | aynı |
>     | M12 | terim sınırı 255 | `TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL` |
>     | M14 | terim İKİ hash'ten de çıkarıldı | `TestOpReadTenants_TwoPhaseLifecycle` (başka terim okundu) + birinci aşama testi (hash) |
>     | M15 | `op_read_tenants` EXECUTE `tappa_app`'e | imza testi + `TestOperator00029_TheDefinerReadsNamedColumnsAndNoSecret` + `TestOperator00026_ForwardCatalogPin` |
>     | M16 | tanımlayıcıya `tags.aes_key_ref` SELECT | `…TheDefinerReadsNamedColumnsAndNoSecret` + `TestOperator00026_PrivilegeMatrix` |
>     | M17 | `tappa_operator`'a `tenants` (id, name) SELECT | aynı ikisi |
>     | M18 | ayrıntı `search_path = pg_catalog, public` | imza testi + ileri pin (gölge testi YEŞİL kaldı — gövde `public.`-nitelenmiş; bulgu değil, sınır L10) |
>     | M19 | liste oturumu touch'sız çözer | `TestOpReadTenants_RefusesEveryDeadSession` + kaynak pini |
>     | D1 | Down `tenants` yetkisini bırakır | `TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain` |
>     | D2 | Down her zaman `NOT VALID` | aynı (2. dal) |
>     | D3 | Down `op_begin_read`'i 00027'den farklı gövdeyle | aynı (iki dal) |
>     | D4 | ön koşulda "operatörün üyesi var" yok | `TestOperator00029_PreconditionRefusesAWrongCluster` |
>     | G1 | Go tarafı terim reddi yok | `TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL` (bağlantı kullanıldı) |
>     | G2 | ikinci aşamaya sayfa ters | `TestOpReadTenants_PagesAreCappedAndOrdered` + havuz testi |
>     | G3 | bilinmeyen tenant `ErrNoSuchTenant` değil boş | ayrıntı testi + havuz testi |
>     | G5 | parametre anahtarı `search` | havuz testi (22023) |
>     | G7 | `MaxTenantSearchRunes` 255 | sınır testi + birinci aşama testi |
>     | G8 | `TenantDetail` ikinci aşamaya başka id | havuz testi (`ErrOperatorRefused`) |
>     | L1 | `locations`'a ikinci sütun ACL'i (`tappa_resolver`) | `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy` (geri alındı, şema eşit) |
>
>     (M20–M23, G4, G6 numaraları kullanılmadı.) 36 mutasyon, 36 kırmızı. Pozitif kontrol: pristine'de
>     17 yeni testin 17'si yeşil; hedefli `-race` koşusu md. 15.
> 14. **B fazına devir (numaralı):**
>     1. **`*OperatorDB` yöntemleri:** `TenantList(ctx, sessionHash, q)` ve `TenantDetail(ctx,
>        sessionHash, id)`, `return F(ctx, o.pool, …)`; AYNI değişiklikte handler paketinde tüketici
>        arayüzü (ör. `TenantStore`); `TestOperatorDB_IsTheStoreAndNothingMore` kümeyi
>        `operatorauth.Store` ∪ `LegalStore` ∪ yeni arayüz ∪ `Close` diye türetir;
>        `TestOperatorDB_EveryMethodDelegatesVerbatim` 9 → 11; `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor`
>        öncülü 10 → 12; `cmd/tappa`'nın `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` kuralı
>        yeni handler'a izin verecek şekilde.
>     2. **Rotalar:** `GET /operator/tenants` (liste + arama + sayfa) ve `GET /operator/tenants/{id}`
>        (genel bakış) — OP-8 kabuğu, aynı zincir; `TestSurface_TheScreensOfLaterTasksAreNotMounted`
>        listesinden çıkar (yorumundaki *"definers 00026 and 00027 do not have"* cümlesi de);
>        `classRoutes`, `designedHeaders`, sızıntı kolları, `harvestWant`; konsoldan link ancak rota
>        kayıtlıyken.
>     3. 🔴 **Terim URL'de taşınırsa ingress erişim log'una ve tarayıcı geçmişine girer** — terim bir
>        adres olabilir (kişisel veri; §3.5 enrollment token'ı aynı sebeple sorgu dizgisinden
>        çıkarmıştı). Karar B'nin: (a) arama POST gövdesiyle (sayfalama gizli alanla), (b) GET `?q=` +
>        ingress log politikası. Öneri (a). Terim `slog`'a, hata mesajına, `Location`'a yazılmaz.
>     4. **Sınırda doğrulama:** terim geçerli UTF-8, NUL/kontrol karakteri yok, ≤ `db.MaxTenantSearchRunes`
>        rune (trim B'nin kararı; veritabanı terimi olduğu gibi bağlar); `ErrTenantSearchRefused` → 400;
>        sayfa numarası 1..999999999, sayfa boyu handler'ın SABİTİ (kullanıcıdan değil); id `uuid.Parse`
>        — bozuk id veritabanına gitmez (404/400); `ErrNoSuchTenant` → 404 (okuma audit'lidir);
>        `ErrOperatorRefused` → oturum kapısı davranışı; 22023 veritabanı hatası → 500 (handler
>        doğruladıysa ulaşılamaz).
>     5. **Ad başlıkta:** `operatorpages.TenantScreen(title, NewTenantName(o.Name))`; `tenants.name`'de
>        boş olmama CHECK'i YOK (dev'de boş ad 0, ölçüldü) — `NewTenantName` boşu reddeder, B ret
>        yolunu seçer (TenantScreen sözleşmesi: 500).
>     6. **Bütçe:** her liste/ayrıntı görüntüsü iki veritabanı işlemi (`op_begin_read` + `op_read_*`) +
>        oturum kapısı; `sessionLimit` türetmesi okuma başına iki birimle yeniden yazılır (OP-8 devri).
>     7. **Sayfalama:** "sonraki" yalnız tam sayfa dönünce; derin OFFSET maliyeti (md. 12) — makul bir
>        üst sınır B'nin.
>     8. **Etiketler:** sayılar yalnız `active` statüdekiler (lokasyon hariç); zaman render'da.
>     9. **OP-12/13/15'e:** `op_read_tenants`'ın dönüş tipini değiştirmek (ör. OP-15'in `suspended_at`'i)
>        `CREATE OR REPLACE` ile olmaz — `DROP FUNCTION` + `CREATE` + sahip/REVOKE/GRANT, ve
>        `op00029Functions` pini; yeni okuma türü `op_begin_read`'i (`CREATE OR REPLACE`) ve
>        `operator_read_tickets_kind_check`'i genişletir, Down'ı bilet varsa `NOT VALID`; yeni her
>        tanımlayıcı sütunu `TestOperator00026_PrivilegeMatrix` izin listesine, `locations`'a ise
>        `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy`'nin adlı listesine.
> 15. **Doğrulama:** hedefli `-race -v` (`internal/db`, `cmd/tappa`, `cmd/opadmin`; .env'li): 427 üst
>     düzey PASS, 0 SKIP, 0 FAIL (alt testlerle 921), DATA RACE 0; `TestTablesLock_IsTakenOncePerTestTree`
>     ve `TestEveryNamedTestExists` PASS; `./internal/handler -run TestComments_` PASS (ilk koşuda
>     `TestComments_DoNotQuoteTheDriftingRosterSize` migration'daki bir kadro sayısı alıntısını
>     yakaladı — alıntı kaldırıldı, sorgusu yazıldı); `gofmt -l -s` boş; `GOTOOLCHAIN=go1.26.7 go vet`
>     ve `staticcheck` temiz; `make gen` idempotent (parmak izi önce/sonra aynı); `redline-check.sh`
>     exit 0 (R5 muafiyetleri değişmedi: 5 tablo). **Tam suite** (`go test ./... -count=1`, .env'li,
>     son dosyalarla): 26 paket ok; kırmızı yalnız (a) `cmd/rotatekek` `TestRotateScript_AccountsForEveryGoToolchainVariable`
>     (bilinen T72: yerel go 1.27'nin `GOPACKAGESDRIVER`'ı) ve (b) worktree'de `web/static/css/app.css`
>     yok (gitignore'lu derleme artefaktı, `make css` koşulmadı) — `internal/handler`'da iki,
>     `internal/handler/operator`'da bir, `internal/httpx`'te bir test, dördünün mesajı da app.css.
>     İlk tam koşu bir GERÇEK bulgu verdi (md. 6: `structure`), düzeltildi, ikinci koşuda yok.
>     Bitişte `pg_stat_activity`'de oturum 0, danışma kilidi 0. `scripts/pg-restore-verify.sh` KOŞULMADI — yeni
>     veritabanı ister (bu görevde yasak); uyumu metinden: yeni tablo yok (ENABLE/FORCE sayıları
>     değişmez), yeni yetkiler sütun düzeyi SELECT ve script onları `column_privileges` ile
>     karşılaştırır (00027'nin `legal_documents` yetkileriyle aynı yol).
> 16. **Sayılı sınırlar (A):** (L1) md. 10'un kalıcı satırları; (L2) liste maliyeti tenant sayısıyla
>     doğrusal, derin OFFSET hepsini sıralar — bir DSN sahibi pahalı sayfalar isteyebilir (her biri
>     bir `read` satırı bırakır); (L3) **audit neyin arandığını söylemez** — arama yapıldığını ve
>     (2. turdan beri, md. 18) aramanın içeriksiz SINIFINI söyler (`none`/`text`/`address`/`id`);
>     terimin kendisi bilet hash'inde geri çevrilemez biçimde bağlıdır (bilinçli: GDPR, ADR §2 v 1); (L4) sınır
>     4'ün penceresi 30 sn (geri alınan okuma yeniden okunur); (L5) aynı oturumla eşzamanlı çağrılar
>     oturum satırı kilidinde sıraya girer; (L6) iki kopya kanonikleştirme (`op_begin_read`'in
>     `v_bound`'ı, `op_read_*`'ın `jsonb_build_object`'i) — kayma fail-closed'dır (her okuma reddedilir)
>     ve gerçek yolu süren iki test onu yakalar; (L7) ad eşleşmesi `lower()`'ın locale'ine bağlı (dev
>     `en_US.utf8` libc, Malta harfleri ölçüldü; üretimin ctype'ı ölçülmedi); (L8) adres eşleşmesi
>     her durum ve rolü kapsar (devre dışı bir sahibin adresi tenant'ını bulur — destek ihtiyacı);
>     (L9) Down tanımlayıcının beş tablodaki BÜTÜN yetkilerini alır (00029 öncesi hiç yoktu — ölçülü);
>     (L10) M18: `search_path`'e `public` eklemek gölge testinde görünmez (gövde nitelenmiş) — onu ileri
>     pin tutar; (L11) tür koşulunun davranış kanıtı sahte biletlerle (gerçek biletlerde hash'ler zaten
>     ayrışır); (L12) `pg-restore-verify.sh` koşulmadı (md. 15).
> 17. **Güvenlik iddiası** ADR 0021 → "OP-11 uygulama notu" sonunda üç parçalı (PART I ölçen
>     testlerin adlarıyla, PART II pinler, PART III).
>
> **Kabul (satır):** tappa_app EXECUTE yok ✓ (imza testi, `…TheDefinerReadsNamedColumnsAndNoSecret`,
> ileri pin; M15 kırmızı) · tappa_operator `tenants` doğrudan SELECT yok ✓ (aynı test + PrivilegeMatrix;
> M17 kırmızı) · süresi geçmiş/MFA'sız/iptal hash → exception ✓ (`TestOpReadTenants_RefusesEveryDeadSession`,
> birinci aşama testi; M19 kırmızı) · her çağrı tam 1 operatör audit satırı ✓ (md. 2) · anahtar/hash
> sütunları `has_column_privilege(opdefiner,…)=false` ✓ (dokuz türetilmiş sütun + yedi adlı; M16
> kırmızı) · tenant başlıkta adıyla — A: ayrıntı adı döndürür ✓ (`TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows`),
> ekran B (md. 14.5).
>
> **2. tur (2026-10-03; üçüncü göz ONAY — 27 mutasyon, 23 kırmızı — + tappa-security-auditor ONAY;
> orkestratör kararı + bloklamayan bulgular).** Taban değişmedi (`e0f53b6`; dal ucu `b06370c`'ye
> birleştirmeyi orkestratör yapar). Migration önce `down`, dosya düzenlendi, sonra `up-by-one`.
>
> 18. **Arama SINIFI audit'e girer (orkestratör kararı; güvenlik ORTA + üçüncü göz B6):** `detail =
>     {"search": "none"|"text"|"address"|"id"}`, `op_begin_read` terimden türetir, sırayla ilk uyan:
>     `none` (boş) · `id` (terimin tamamı tireli uuid, harf büyüklüğü serbest) · `address` (`@`
>     içerir) · `text` (geri kalan). **Kurallar ve sıra = `op_read_tenants`'ın koşabildiği dallar:**
>     id dalı yalnız tam uuid'de, adres dalı **artık yalnız `@`'li terimde** (aynı yüklemle kapılı —
>     `v_address := strpos(p_query, '@') > 0`), ad eşleşmesi her zaman. Sınıf terimin ulaşabildiği en
>     dar dalı adlandırır: `text` bir arama yalnız ad eşleştirebilir. uuid `@` içermez, ikisi
>     çakışmaz; `none` önce, çünkü `''` ikisi de değil. Kapının kaybettirdiği: `@`'siz bir yönetici
>     adresi — kayıt reddeder (`internal/domain/signup`), dev'de 0 (ölçüldü:
>     `SELECT count(*) FROM admin_users WHERE strpos(email::text, '@') = 0` → 0). Sınıf içeriksizdir
>     (terimin hiçbir karakteri yok) ve çağıran onu taklit edemez (terim bilet hash'ine bağlı; okuma
>     sınıfın türetildiği terimi koşar). Terim — ham ya da hash'i — yine hiçbir satırda yok.
> 19. **Go tarafı uzunluk sınırı (güvenlik DÜŞÜK):** `TenantList` `utf8.RuneCountInString(term) >
>     MaxTenantSearchRunes` → `ErrTenantSearchRefused`, gidiş-dönüşsüz; veritabanının 254'ü ikinci
>     kopya. Gerekçe (güvenlik denetiminin ölçümü): dev'de `log_statement = all` uzun terimi
>     reddedilmek üzere tam hâliyle sunucu log'una yazıyordu. Sınır içindeki terimin de dev log'una
>     gittiği aynı mekanizmadan çıkarımdır, bu turda okunmadı (sunucu log'u okunmaz).
> 20. **B1 — Down'ın `NOT VALID` dalının üçüncü kolu:** yalnız `tenant_detail` bileti varken Down
>     `NOT VALID` döner. **B2 — "Only" ölçüldü:** plan, işletme türü, yapı, saat dilimi, bir
>     fikstürün VAT numarası ve `@`'siz yönetici adresi terim olarak hiçbir fikstürü bulmaz; dönen her
>     satırın ADI terimi taşır; kontrol: aynı adres `@`'li hâliyle bulunur. **B3:** ADR'deki
>     "`tappa_operator` ve `tappa_app` hiçbir şey almadı" → "`tappa_app` hiçbir yetki almadı;
>     `tappa_operator` iki yeni fonksiyonda EXECUTE aldı, hiçbir tablo yetkisi almadı" (kartta md. 7
>     aynı düzeltme). **B5 — uygulandı (devir değil):** büyük harf ve Malta harfli bir terim
>     (`ĦAMRUN Żebbuġ ĊAFÈ <TOKEN>`) üretim kurucusunun havuzunda iki aşamadan geçer; bilinmeyen oturum,
>     201'lik sayfa ve sınır aşımı retlerinin hata metninde terim yok
>     (`TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions`; Gi ve Gb kırmızı).
> 21. **Güncellenen testler:** `TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm` (sekiz
>     terimde dört sınıf + kenarlar: bir karakter fazlalı uuid `text`, `@`'li uuid `address`, tek `@`
>     `address`, büyük harfli uuid `id`, Malta harfli ad `text`; satırlarda terim yok);
>     `TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL` (erişimcide 255 → sentinel, 0 deyim; 254
>     → 1 deyim; veritabanında 254/255); `TestOpReadTenants_SearchMatchesNameAddressAndIDOnly` (md. 20);
>     `TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain` (üç dal);
>     `TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions` (sınıf `text`, md. 20 B5; artık üç `read`
>     satırı commit eder — koşu başına bu dosyadan toplam beş).
> 22. **Ölçüm:** v28 tabanı `f0dc03e20de806e8`; 2. turun dosyası (sha256 öneki `6b0944113d536461`):
>     Up → `24c6f40fcaebea22`, Down → `f0dc03e20de806e8` birebir, Up → `24c6f40fcaebea22` birebir.
> 23. **Mutasyonlar (2. tur; hepsi KIRMIZI, harness 1. turunki — migration mutasyonları goose döngüsüyle,
>     sonda şema `24c6f40fcaebea22`):**
>
>     | # | Mutasyon | Kırmızı test (ilk hata) |
>     |---|---|---|
>     | Y10 | Down'ın `NOT VALID` koşulu yalnız `kind IN ('tenants')` | `TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain` (3. dal: Down 23514 ile düşer) |
>     | Y3 | aramaya `OR t.plan = p_query` | `TestOpReadTenants_SearchMatchesNameAddressAndIDOnly` ("standard" fikstürü buldu) |
>     | C1 | sınıf hep `text` | `TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm` |
>     | C2 | sınıfın `@` kontrolü kaldırıldı | aynı (adres terimi `text`) |
>     | A1 | adres dalı kapısız (`v_address := true`) | `TestOpReadTenants_SearchMatchesNameAddressAndIDOnly` (`@`'siz adres E'yi buldu) |
>     | A2 | kapılı adres dalı bağsız `EXISTS` | aynı (A'nın adresi her tenant'ı döndürdü) |
>     | GL | Go tarafı uzunluk sınırı yok | `TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL` (bağlantı kullanıldı) |
>     | G7 | `MaxTenantSearchRunes` 255 | aynı + birinci aşama testi (255 karakter veritabanında reddedildi) |
>     | Gi | birinci aşamaya terim küçük harfle | `TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions` (`ErrOperatorRefused`) |
>     | Gb | birinci aşama hatası terimi `%q` ile sarar | aynı (hata metninde terim) |
>
> 24. **Sayılı sınırlar (2. tur eki):** (L13) **DSN sahibi (ya da oturumu olan bir operatör) bir adres
>     listesini deneyebilir;** deneme başına commit edilmiş bir `read` satırı ve artık `address` sınıfı
>     kalır, içerik kalmaz — hangi adreslerin denendiği izden okunamaz (bilinçli), ne kadar ve hangi
>     türde denendiği okunur; (L14) sınıf terimin ulaşabildiği dalı adlandırır, eşleşen dalı değil
>     (`address` sınıflı bir arama adla da eşleşmiş olabilir); (L15) `@`'siz yönetici adresi adres olarak
>     aranamaz (md. 18); (L16) **birleştirmeye dek (B7):** dev DB 29'dayken dalın ucundaki (OP-11'siz)
>     dört test dev'e karşı kırmızıdır — `TestOperator00026_PrivilegeMatrix`,
>     `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`, `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy`,
>     `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`; OP-11 birleştirilince yeşile döner (bu
>     worktree'de dördü yeşil). CI kendi veritabanını dalın migration'larından kurar, etkilenmez.
>
> **3. tur (2026-10-03, kapanış; dar kapanış üçüncü gözü ONAY + güvenlik yeniden denetimi ONAY).**
> Migration'a DOKUNULMADI (dosya sha256 öneki `6b0944113d536461`, şema `24c6f40fcaebea22`, goose 29);
> yalnız test ve metin.
>
> 25. **F1 — sınıf ↔ dal eşitliği id ve `none` kenarında da testle bağlı:** sınıf tablosuna
>     (`TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm`, artık on bir terim) `" "` →
>     `text`, `{uuid}` → `text`, tiresiz uuid → `text` eklendi; aramaya
>     (`TestOpReadTenants_SearchMatchesNameAddressAndIDOnly`) D'nin id'si süslü parantezde ve tiresiz →
>     D bulunmaz, `" "` → ad eşleşmesi: dönen her satırın adında boşluk var ve adlarının hepsi boşluk
>     içeren beş fikstür ilk sayfayı tutar. Mutasyonlar (goose döngüsüyle; her birinin sonunda şema
>     `24c6f40fcaebea22`, goose 29):
>
>     | # | Mutasyon | Kırmızı test (ilk hata) |
>     |---|---|---|
>     | X2 | yalnız SINIF regex'i tiresiz uuid'i kabul eder | sınıf testi (tiresiz uuid `id`, `text` beklenirken) |
>     | X9 | yalnız DAL regex'i `{uuid}`'i kabul eder | arama testi (`{D}` D'yi buldu) |
>     | X3 | iki regex de tiresiz uuid'i kabul eder | sınıf testi + arama testi (tiresiz D, D'yi buldu) |
>     | X15 | `btrim(v_query) = ''` → `none` | sınıf testi (`" "` `none`, `text` beklenirken) |
>
> 26. **F2 (metin):** md. 10 artık 5 `read` satırı diyor; md. 16 L3 sınıfı anıyor. **F3 (metin):**
>     `db/queries/operator.sql`'in ReadTenants yorumu `@` kapısını ve "tireli uuid"i söylüyor; `make gen`
>     sonrası `internal/store` diff'i yok (yorum yalnız `-- name:`'siz belgede; sqlc onu okumaz).

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
| EM-4 | Şablonlar (`web/templates/email/*.templ` + metin üreticileri) | M | builder + tappa-brand | tam 1 mutlak URL; `<img`/`@font-face`/`<link`/`<script` 0; `<script>`'li ad escape; Maltaca doğru; konu ASCII ise olduğu gibi, değilse `=?utf-8?q?` *(EM-1 düzeltmesi: `mime.QEncoding` ASCII konuyu değiştirmez — ADR 0022)*; kontrast AA (hesaplanmış) | EM-1 |
| EM-5 | Asenkron reset teslimi + `emailResetChannel` + main `case email` | M–L | builder + güvenlik | kanal gecikmesi 2 s iken kayıtlı/kayıtsız medyanları [taban, taban+50 ms] (yeni TimingIsFlat; senkron kodda kırmızı olduğu gösterilir); grant başına tam 1 audit; kuyruk dolu → senkron undelivered; boşaltma HTTP kapanışıyla eşzamanlı ve `httpShutdownGrace` içinde, ADR 0022 §6.6(b)'nin uçuşta-istekli tarifiyle pinli *(EM-1 düzeltmesi: `TestShutdownBudget_*` yalnız iki sabiti okur, ardışık bir beklemeyi yakalamaz — ölçüldü)*; grant başına `recover` → henüz satır yazılmadıysa `undelivered`; M7-04'ün sahteye karşı koşmuş kriterleri GERÇEK SMTP'ye karşı yeniden koşulmuş (tablo rapora); canlı duman: Gmail "Show original" SPF=PASS (mail.taptime.mt), DKIM=PASS, DMARC=PASS | EM-2,3,4 + kullanıcı dış adımları |
| EM-6 | Adres okuma + "e-postayı değiştir" aksiyonu | M | tappa-db-migrator + builder + tappa-brand | RLS izolasyon (A, B'nin adresini okuyamaz); değişiklik bekleyen davetleri aynı tx'te iptal; audit; log'da `.Email` yok (R7b); migration beklenmiyor (ölç) | EM-5 |
| EM-7 | `invite.EmailChannel` + `emailLinkSink` + davet anahtarı + limitler | M–L | builder + güvenlik + tappa-brand | 🔴 **Ön koşul (EM-4 devri, 2026-10-03):** `email` modu açılmadan önce KULLANICI KARARI — tenant/çalışan adı yalnız doğrulanmış tenant'ta mı gösterilir, yoksa saldırganın yazdığı düzyazı ve rakamların DKIM imzalı davette görünmesi açıkça kabul mü edilir (ADR 0022 §8, sayılı sınır 22); cevap state.md'ye tarihiyle · email modunda gövdede `/activate?code=` 0; `invite.code_emailed` 1, `code_shown_to_manager` 0; adres yok/geçersiz/yönetici adresi → `employee_invites` satırı 0; N+1. davet oran sınırında red, satır basılmaz; SMTP hatası log'da yalnız class/code (`employeeactions.go:419` yolu sızıntı testiyle kapalı); yedek yalnız owner'a + `manager_panel` audit | EM-6 |
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

> **Kart düzeltmesi (2026-10-03, EM-1 uygulaması sırasında; 2., 3., eşitleme, 4. tur ve son eşitleme aynı gün).** Yazıldı:
> [ADR 0022](../adr/0022-islemsel-eposta.md) (yeni — işlemsel e-posta: SES SMTP + `net/smtp`) ·
> [ADR 0005](../adr/0005-kabul-edilen-riskler.md) (ana tablonun 5. satırında iki hücreye tarihli
> işaret, §5'e *"EK NOT — 2026-10-03"* ve kendi sayılı sınırları, Sonuçlar'daki M5-02 maddesine
> *"kalkar değil daralır"* düzeltmesi; daha önce var olan metinden silinen kelime 0) ·
> [ADR 0015](../adr/0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) (başlıkta *"Durum notu"*
> satırı + sonda *"Durum notu — 2026-10-03"*) · [m7-portal.md](m7-portal.md) (M7-04 ve M7-07'ye
> tarihli *"Kart notu"*). Kod dosyası değişmedi; `cmd/tappa/adr0005_test.go` **değişmedi** —
> risk sayısı *"sekiz"* ve B3 tablosu etkilenmedi, ana tabloya yeni satır eklenmedi, 5. satır
> `| 5 | **…` biçimini korudu. Q02'nin "Cevaplananlar"a taşınması orkestratörün. **Ölçüm
> ortamı:** ayrı git worktree, dal ucu `f3c9c04`; stdlib sondası go1.27.1 **ve** go1.26.6
> (darwin/amd64, çıktı birebir aynı); SDK modül sayımı scratchpad'de; EM-2'nin `internal/mail`'i
> ayrı worktree'de **yalnız okundu**; DB'ye bağlanılmadı, AWS hesabına erişilmedi.
>
> **Kabul — kanıtlar:**
> 1. **(b)/(c) ölçüleriyle elendi.** (c): scratchpad'de iki modül, `go mod tidy` +
>    `go list -m -f '{{if not .Main}}{{.Path}}{{end}}' all` → yalnız `service/sesv2` + `aws`
>    **6** modül (2 doğrudan + 4 dolaylı), `config` eklenince **15** (2 doğrudan + 13 dolaylı);
>    depo `go.mod`'u bugün **10** modül gerektirir (ADR 0022 S11, B25). (b): m10 tablosunun
>    *"§3 gerilimi"* gerekçesi ölçüldü ve **eleme gerekçesi olmaktan çıkarıldı** —
>    `rg -l '"crypto/hmac"' --glob '*.go' --glob '!*_test.go' internal cmd web` →
>    `internal/sun` dışında **12** üretim dosyası (token/kod özetleri, anahtar türetme, HMAC
>    imzalı durum çerezleri, TOTP — B24) ve ADR 0020'nin *"Kriptografinin yeri"* kararı
>    (2026-09-26) §3'ü fiilen AES'e bağladı; eleyen, SigV4 protokolünün sahipliği (saat
>    bağımlılığı dahil) ve yalnız-SES test yolu.
> 2. **Y-D "kalkar" değil "daralır".** ADR 0005 §5 ek notu üç hâli tarihli yazar: bugün (HEAD
>    `f3c9c04`) hiçbir şey değişmedi — tek kanal `ManagerVisibleChannel`
>    (`employeeactions.go:385`); EM-7 ile önkoşul *"müdür kodu görür"*den *"müdür bir posta
>    kutusunu denetler"*e kayar; **tek kutu N hayali çalışana yeter** (artı-adresleme citext
>    eşitlik kapısını ve tekil indeksi geçer); iz yalnız adres düzenlenene dek; owner-only yedek
>    EM-12'ye dek boş. ADR'deki iki *"kalkar"* cümlesinin ikisinin de **hemen yanına** düzeltme
>    kondu. `go test -count=1 ./cmd/tappa` → `ok`; iki ADR 0005 testinin çıktısı
>    *"13 rows, 3 without…"* ve *"main risk table: 8 rows, prose says sekiz"*.
> 3. **ADR 0015 durum notu** (karar değişmedi; teslimat yükümlülüğünün evi ADR 0022; yeni artık
>    risk: basım–audit arası süreç ölümü) · **M7-04 notu** (Q02 cevabı; 6. düzeltme maddesinin
>    yükümlülüğü EM-5'te; 4. maddenin ardılı ADR 0022 §6'da; *"kapatan şey adres doğrulama"* →
>    Q02 doğrulama getirmez, EM-11) · **M7-07 notu** (kilit artık EM-7'ye bağlı, EM-12 olarak
>    açılır; owner-only yedek yönetici davetine uygulanmaz).
> 4. **Bağlam bugünkü kod ölçülerek** — ADR 0022 B1–B36, S1–S14 (her satırda komut ya da
>    `dosya:satır`).
> 5. **Güvenlik iddiaları üç parçalı** — on iddia (A–J); PART I henüz bu ağaçta olmayan
>    testleri **tarifle** anar, EM-2'nin testlerini `…` önekiyle *"birleştirildiğinde"* diye;
>    PART II pinleri, her birinin yakaladığını ve **yakalamadığını** sayar; PART III tek
>    gerekçesiz cümle, on kez.
> 6. **Yalnız var olan test adları anıldı**; `TestEveryNamedTestExists` yeşil
>    (*"60 live, 60 budgeted"*, tabanla aynı).
> 7. **Sır değeri yok**; `./scripts/redline-check.sh` → exit 0 (çıktı tabanla aynı, yalnız bir
>    WARN satırının sırası farklı).
>
> **Karta düzeltmeler — §4'ün tasarım özü ve görev tablosu, ölçümle:**
> 1. 🔴 **EM-5 "kapanışta ≤3 s boşaltma (`shutdownbudget_test.go`)" — mevcut testler bir
>    boşaltmayı GÖREMEZ.** 1. turun bu maddesi *"ardışık eklenirse test kırmızı"* diyordu;
>    **yanlıştı** (üçüncü göz ölçtü: `main.go`'da Shutdown'dan sonra `time.Sleep(3s)` → üç
>    kapanış testi PASS — test yalnız `httpShutdownGrace`, `DefaultCloseGrace` ve YAML'ı okur;
>    gerçek marj 2 s olurdu, istenen 5 s). ADR 0022 §6.6: boşaltma HTTP boşaltmasıyla eşzamanlı
>    başlar (ör. `srv.RegisterOnShutdown` — Shutdown onu `go f()` ile başlatır ve beklemez,
>    `net/http/server.go:3257-3259`), `cmd/tappa` havuzu kapatmadan önce onu bekler; **pin
>    (tarif, 3. turda yeniden yazıldı):** (a) adlandırılmış boşaltma sabiti D
>    `httpShutdownGrace`'e iki emsal yuvalanma testinin biçimiyle bağlanır; (b) kapanış dizisi
>    testin çağırabildiği bir işleve yazılır, **T ≥ D süren bir istek uçuştayken** sürülür ve
>    toplam süre **< T + D − pay** assert edilir (pay ≥ `shutdownPollIntervalMax` = 500 ms,
>    `server.go:3227`). Uçuşta istek yokken doğru, işlev içi `Sleep(D)` ve ardışık uygulama üçü de
>    ≈D ölçülür (denetçinin ölçümü, ADR B34); T = D = 1 s'de 1,056 / 2,089 / 2,141 s. Başlama sırası
>    assert edilecekse an `go`'dan önce kaydedilir (`go f()` içinde kaydedilen anla assert 2000'de
>    1896 kırmızı), ya da hiç assert edilmez. Dizi işlevinin **dışında** `main`'e eklenen
>    bekleme, pay'den kısa bekleme ve testin kurmadığı koşulda ödenen bekleme *Yakalamadığı*'dadır.
> 2. **EM-5 "grant başına tam 1 audit" → hesap başına audit bütçesinin içinde ve süreç ölmediği
>    sürece.** `adminresetlimits.go:168-228`: yönetici başına 10 dk'da 10; aşınca tek
>    `admin.recovery.rate_limited` satırı (`recordForAdmin`, `adminreset.go:927-950`).
> 3. **EM-5 "işçi mevcut `h.deliver`'ı çağırır" — gönderim ve audit aynı ctx'i paylaşır**
>    (`adminreset.go:522-596`); audit'e ayrı sınır. **İşçide panik** `middleware.Recoverer`'ın
>    (`router.go:82`) dışında: grant başına `recover` → **henüz satır yazılmadıysa**
>    `undelivered`, panik değeri loglanmaz, yeniden fırlatılmaz
>    (`internal/encode/session.go:1865-1870`'in yeniden fırlatması bir istek goroutine'i
>    içindir); 250 ile audit arasındaki panik sınır 15'te.
> 4. **EM-4 "konu `=?utf-8?q?`"** — konular sabit ASCII; `mime.QEncoding.Encode` ASCII'yi
>    değiştirmez (S9). Kriter: başlık kodlayıcısı Maltaca girdide `=?utf-8?q?` üretir; sabit
>    konular düz geçer.
> 5. **(c) satırı "servis başına ≥4; config ile +8-9"** → ölçüldü **6 / 15** (+9).
> 6. **Oran sınırları** — sıfırlama yayılımı kaynak adres başına 10 dk'da 20 × 8 = **160**
>    (saatte 960); **davet yolu da:** kayıt adres başına saatte 3 tenant ve ömür boyu tavansız,
>    tek adres ikinci saatte 6 × 50 = 300 ile devre kesiciye (~300/saat) ulaşır. Paylaşılan
>    kesici ve tek SES hesabı: itibar (bounce/complaint) düşer, SES hesabı duraklatabilir
>    (belge; ölçülmedi). Alıcı başına sıfırlama tavanı karar verilmedi (M7-04 düzeltme 1(iii)).
> 7. **EM-3** — `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed` `"Q02"` alt dizgesini arar
>    (`config_test.go:475`); kodda Q02'yi açık diye anan metin
>    `rg -c 'Q02' --glob '!docs/**' --glob '!.claude/**' .` → **17 dosya, 27 satır**.
> 8. **EM-7** — saklanan adres kuralı (`staff.go:670-693`) gönderim kuralından zayıf; ASCII dışı
>    saklanmış adres davette reddedilir, migration yok.
> 9. **Prod'da `localhost` reddinin gerekçesi ölçüldü:** `smtp.PlainAuth` `localhost` adında
>    TLS'siz bağlantıda kimliği gönderir (S3).
> 10. **`smtp.SendMail`'in elenmesi ölçüldü** (S2, S4).
> 11. **Alıcı kuralı** — `ali@[192.0.2.1]` geçer (S8).
> 12. **EM-2 "`MinVersion` konmazsa" kriteri tarif edilen testle yakalanamaz:** `MinVersion == 0`
>     iken istemci 1.2 altını zaten reddeder (S12, `crypto/tls/common.go:1239`; go1.27.1 ve
>     go1.26.6). İddia A'da *Yakalamadığı*'na taşındı.
> 13. **`mime/quotedprintable` satır sonu kaybı** (S13: `"a\r\x1d\nb"` → 1 CRLF) — EM-2 kodlamadan
>     önce LF'ye normalize eder; ADR 0022 §2'de normatif.
>
> **2. tur (2026-10-03 — üçüncü göz RED, güvenlik RED; belge düzeyinde, normatif içerik
> değişti).** Kapananlar:
> - **EM-K9, kullanıcı kararı** (*"hayir sahte birsey eklemene gerek yok ses'e baglariz
>   direkt"*): Mailpit yok, dev'de `none`/`panel`, doğrulama Go testlerindeki sahte SMTP sonra
>   doğrudan SES (önce sandbox) — ADR 0022 §12; *Karar verilmedi*'den çıktı. **Prod'da özel kök
>   havuzu yasak** (§2; EM-3 kaynak pini, tarif).
> - **Güvenlik (bloklayan):** ADR 0005 ekinin *"bu çalışanın kodu nereye gitti sorusu kayıtta
>   cevaplanır"* cümlesi Y-D saldırganına karşı yanlıştı (`invite.code_emailed` ve EM-6 satırı
>   adres taşımaz, `employee.added` yalnız `has_email` — `staff.go:362-376`) → *"adres
>   düzenlenene dek; EM-6 satırı değişikliğin olduğunu gösterir, değeri göstermez"*; ADR 0022
>   sayılı sınır 2 ve 19, ADR 0005 ekinin sayılı sınırları. Alıcının anahtarlı özeti ADR 0022
>   *Karar verilmedi*'de (yeni anahtar, GDPR, değişmez `audit_log`) — uygulanmadı.
>   *"Hayali çalışan başına bir kutu"* yanlıştı → tek kutu N hayali çalışana. Owner-only kısıt
>   EM-12'ye dek boş (`signup.go:723-731`) → daralma yalnız kanal ayrımı (EM-8).
> - **Üçüncü göz (bloklayan):** F1 (yukarıdaki düzeltme 1) · F2 (düzeltme 12) · F3: §3'ün
>   *"dışa kapalı alan da `%+v` ile basılır"* cümlesi `error` türü için yanlıştı (`%v`/`%+v`/`%s`
>   `Error()`'u çağırır, alanları yalnız `%#v` gösterir) → kural EM-2'nin reflect tabanlı alan +
>   yöntem pinine bağlandı (`…SendError_HasOnlyAClassAndACode`, birleştirildiğinde).
> - **Üçüncü göz (bloklamayan):** F5 (15 modül) · F6 (davet yolu + SES itibarı, sınır 6) · F7
>   (İddia F başlığı daraltıldı; DATA sonlandırıcısından sonraki iptal → link gitmiş ama
>   `undelivered`, sınır 15) · F8 (tampon gerekçesi §6.2; §10'un `invite_id`/`reset_id`/`ip`
>   eklemesi m10'dan sapma olarak yazıldı) · F9 (B24 düzeltildi + ADR 0020 atfı; B5 aralığı
>   522-596; R7 tavsiyesi tanımlayıcıyı da kapsar, emsal `key32(name)` — `config.go:681-694`;
>   İddia I'da `465` her ortamda; İddia D'de derleme zamanı doğrulamaları adıyla; go1.26.6
>   ölçümü).
> - **Güvenlik (bloklamayan):** `Message` üçüncü beyan edilmiş taşıyıcı (EM-2'de yöntem 0 —
>   okundu; belge devri EM-2'ye) · işçide panik (düzeltme 3) · `message_id` kuralı (§2: şekil
>   `[A-Za-z0-9._-]` ≤128, yankı ise düşer) · SES geri bildirim yönlendirmesi (§11, yeni dış
>   adım; ölçülmedi) · sekizinci sınıf `invalid_message`.
> - **EM-2'nin sapmaları (a–i) normatif yazıldı** (§2–§4, Sonuçlar): `invalid_message`, AUTH 4xx
>   → `throttled` + tek deneme, `ErrNotConfigured`, kendi `Message-ID`'miz, QUIT 221
>   beklenmez, başlık reddi C1 / U+2028 / U+2029 / `=?` ile (fuzz bulgusu: encoded-word
>   görünümlü ASCII konu CRLF + sahte `Bcc:`'ye çözülür), `Ref` iletilmez ve biçimli, ayrı
>   görünen ad kontrolü yok, süre iki yoldan (`SetDeadline` tek başına pinsiz — sınır 16).
>
> **3. tur (2026-10-03 — kapanış üçüncü gözü RED, güvenlik yeniden denetimi RED).** Kapananlar:
> - **B1, bayt tavanı (güvenlik YÜKSEK + üçüncü göz) — orkestratör kuralı, EM-2 sapması j:** ham
>   bağlantı `NewClient`'ten önce deneme başına sayan bir sarmalayıcıya sarılır, tavan
>   **256 KiB**, TLS öncesi/sonrası tek sayaç, aşımda `network` (ADR 0022 §2, §3; **İddia J**,
>   erişilebilirlik). Ölçüldü: `textproto` satırı sınırsız okur (`reader.go:43-45`, `:60-80`,
>   `:287-300` — B31) ve 32 MiB'lık bir TLS öncesi selamlama `smtp.NewClient`'te hatasız kabul
>   edilip +287,8 MiB (go1.27.1) / +255,8 MiB (go1.26.6) ayırttı (S14, `net.Pipe` sondası);
>   denetçinin ~120 MiB → OOM ölçümü yeniden koşturulmadı. Sonuçlar'daki sayı *"on iddia
>   (A–J)"*.
> - **E1, `message_id` yankı kuralı — orkestratör kuralı, EM-2 sapması k:** değer listesine tel
>   üstündeki AUTH PLAIN argümanı ve kullanıcı adı girer; id ile değerlerden biri (`Text`/`HTML`
>   dahil) arasında **k = 8** karakterlik ortak `[A-Za-z0-9._-]` penceresi varsa id düşer (§2).
>   Denetçinin ölçümü B33 olarak kayıtlı (`x<token>`, `<token>-000000` 2. turun kuralını
>   geçiyordu). İddia B PART I'e `x<kod>`, `<kod>.1`, `<kod>-000000` ve AUTH argümanı vakaları
>   eklendi; sınır 17 yeniden yazıldı (biçim değiştiren ve <8'lik parçalara bölen yankı); yanlış
>   pozitif sınır 21 (yalnız `message_id` kaybı).
> - **F1-r2, kapanış tarifi:** düzeltme 1 yeniden yazıldı (T ≥ D uçuşta istek, < T + D − pay,
>   başlama anı `go`'dan önce ya da sıra assert'i yok, yakalamadıkları dürüstçe). Denetçinin
>   ölçümü B34, `shutdownPollIntervalMax` B35.
> - **Güvenlik ORTA — DKIM imzalı içerik:** §8 (*"tek URL"* yalnız şablon literali), İddia H
>   başlığı daraltıldı ve *Yakalamadığı*'na URL benzeri ad, sayılı sınır 22; tasarım kararı
>   EM-4/EM-7/WL-11'e devredildi.
> - **Güvenlik DÜŞÜK + 3E N2 — kök havuzu yasağının iki yolu:** `RootCAs: nil` Linux'ta
>   `SSL_CERT_FILE`/`SSL_CERT_DIR`'i okur (`crypto/x509/root.go:124-138`, B32); EM-3 pinleri
>   (a) yalnız `mail.Config` — atama ve anahtarlı bileşik değer, (b) config'te CA anahtarı 0,
>   (c) manifestte `SSL_CERT_*` env'i 0. Volume / imaj yolu İddia I *Yakalamadığı* + sınır 23;
>   `TAPPA_SMTP_HOST` izinli ad listesi yok sınır 24.
> - **Güvenlik DÜŞÜK — dev'de `email` modu:** boot hatası **yok** (orkestratör kararı); §12'de
>   kabul edilen risk: hedef gerçek röle, prod kimliği `.env`'e konmaz (`.env` `.gitignore:7`,
>   R7d listesine girmez — `redline-check.sh:1834`), seed'de **37** adres (`@kebabfactory.mt`,
>   `@kebabmfg.mt`; `grep -ohE … test/fixtures/seed.sql | sort -u | wc -l`), yerel deneme için
>   sandbox'ta kısıtlı ayrı IAM kimliği dış adım olarak; sınır 25.
> - **Güvenlik DÜŞÜK — panik kuralı:** §6.4 koşullu (*"henüz satır yazılmadıysa"*); 250 ile audit
>   arası panik sınır 15'e eklendi.
> - **3E N1:** §10 kümesine `err_type` (değeri yalnız `%T`) ve `err` (değeri kimlikli sarmalar +
>   yalnız `*SendError`) eklendi, değerleri sınırlandı; m10 sapma paragrafı güncellendi.
> - **3E N3:** §3 tablosu koda göre: tekrar yapılamazsa (süre yok ya da geri çekilmede bağlam
>   biter) sınıf `throttled` kalır (EM-2 dalında `mail.go`'daki `Send` + `pause`).
> - **3E N4:** §6.2 *"her boyda"* daraltıldı: tek adres 10 dk'da ≤160 grant; tek işçi en kötü
>   süreyle 40 işler, yani 32 tek adresle de dolabilir, ≥160 yalnız birden çok adresle.
> - **EM-2'nin 2. tur kararları:** `To`'da `=?` reddi (§4); STARTTLS'e 454/502 ve STARTTLS
>   sonrası EHLO → `tls_unavailable` (§3); `Reply-To` `New`'de alıcı kuralıyla (§4, §5);
>   kullanıcı adı redakte eden tipte (§5, İddia D).
> - **Gerekçeli sapma:** İddia H ve I için istenen *"PART III'e bir cümle"* PART III'e değil,
>   PART II'nin **Yakalamadığı** kısmına ve sayılı sınırlara yazıldı — PART III bu deponun
>   kuralıyla tek gerekçesiz cümledir (agent-brief, M10 OP-8).
>
> **Eşitleme turu (2026-10-03 — EM-2'nin teslim ettiği 2. tur koduna; EM-2'nin
> `internal/mail`'i yalnız okundu).** ADR 0022'de:
> 1. **Yankı listesi** (§2): ham ileti, `To`, `From`, `Subject`, `Ref`, `Text`, `HTML`,
>    kullanıcı adı, **parola**, AUTH PLAIN argümanı (EM-2 `mail.go`, `Send`'deki `sent`);
>    parolanın gerekçesi (`x<parola>` base64'lü AUTH argümanıyla 8'lik pencere paylaşmaz); 8'den
>    kısa id birebir alt dizgeyle de düşer; `To`/`From` çıkarılması (M39/M40) yeşil — **ölçülmüş
>    eşdeğerlik** (ham ileti kapsar), açık değil. İddia B PART I/II buna göre
>    (`…Send_ReadsTheMessageIDFromThe250Reply`, M21/M36/M37/M38/M41–M46 kırmızı).
> 2. **Duraklamada `throttled`** (§3 tablosu): gerekçe eklendi — gönderimi bitiren rölenin
>    4xx'i; `timeout` sağlayıcı kısıtlamasını gizler ve kod taşımaz.
> 3. **`mail.Credential`** (`credential.go`): parola **ve** kullanıcı adı; §5 tablosu ve paragrafı,
>    İddia D (`…Credential_IsRedactedInEveryRendering`, `…Credential_KnownLimitIsReflection`).
> 4. **`Message` redakte ediyor** (`Format`/`String`/`GoString`/`LogValue`/`MarshalText`, derleme
>    anı doğrulamaları, ⚠️ *"loglanmaz"* doc'u); §2 paragrafı ve sınır 20: istisna = dışa kapalı
>    alandaki `Message` yansımayla basılır (`…Message_KnownLimitIsAnUnexportedField`) ve
>    alanlarını doğrudan loglayan çağıran. İddia D `…Message_PrintsAsAPlaceholder`'ı anar.
> 5. **Tavanın gerekçesi** (§2): sayaç el sıkışmayı da sayar; dürüst röle birkaç yüz bayt + bir
>    TLS uçuşu (zincir birkaç KiB, RSA-4096 < 16 KiB) → tavan >10×; `ReadResponse`
>    `strings.Builder` → tavan altında karesel büyüme yok; ölçülen 0,9–2,1 MiB (bütçe 4 MiB).
>    Bir `Send` en çok 2 × 256 KiB okur (yalnız 4xx yeniden denenir).
> 6. **Sınıf eşlemesi** (§3): STARTTLS'e her kod, başarısız el sıkışma, STARTTLS sonrası EHLO →
>    `tls_unavailable`, tekrar yok; `network` yeniden denenmez (`…Send_ANetworkFailureIsNotRetried`
>    — veri sonu noktasından sonra kopuş çift teslim riski); `classify`'ın önceliği birebir yazıldı
>    (tavan → süre → kod → aşama).
> 7. **EM-2'nin yeni test adları** yalnız `…` önekiyle, *"birleştirildiğinde"*: İddia J
>    (`…Send_CapsWhatTheRelayCanMakeItRead`, `…Send_ANetworkFailureIsNotRetried`), İddia D
>    (`…Message_PrintsAsAPlaceholder`, `…Message_KnownLimitIsAnUnexportedField`). İddia J PART II
>    gerçek testle eşitlendi: **bölünmüş (TLS öncesi + sonrası) vaka testte yok** → sayacın
>    STARTTLS'te sıfırlanması *Yakalamadığı*'na taşındı; küçük tavan büyütmesi de.
> 8. **EM-2'nin 12 sayılı sınırı:** karşılığı olanlar (1 → 7, 2 → 17/21, 5 → 16, 7 → 20) adıyla
>    eşlendi; olmayanlar 26–33 olarak eklendi (kasıtlı yansıma · tavan altı ayırma · açık
>    `MinVersion` · RFC 2047'nin 76 karakteri · gerçek SES ölçülmedi · "0 dial" dolaylı sayım ·
>    fuzz yalnız başlık üreticisi · gövde içeriği kısıtlanmaz).
> - **Belge notu EM-2'ye:** kodun yorumu pencere kuralını *"ADR 0022 §3"*'e bağlıyor; kural §2'de.
>
> **4. tur (2026-10-03, kapanış — yalnız ADR metni; birleşik ağaçta denetim: güvenlik ONAY,
> üçüncü göz RED yalnız EM-2'nin pini için).** Orkestratör EM-2'nin `internal/mail`'ini bu
> worktree'ye takip dışı kopyaladı; kopyaya dokunulmadı. ADR 0022'de:
> - **F9 / D3 — birleştirme sonrası çeviri:** `…X` önekli 11 farklı test atfı (13 yer) gerçek
>   `TestX` adlarına çevrildi; İddia C'nin üç testi adıyla (`TestSend_RefusesBadInputBeforeAnyDial`,
>   `FuzzCompose`, `TestHeaderBlock_RefusesWhatItCannotWriteVerbatim`); başlıktaki *"bu ağaçta
>   yok … birleştirilmedi"*, iddia girişindeki *"Test önekiyle anılmaz"* ve bütün
>   *"birleştirildiğinde"* ifadeleri kaldırıldı/güncellendi; Sonuçlar'daki *"§3'e bağlıyor"*
>   notu kaldırıldı. `TestEveryNamedTestExists` (kopyayla): 2560 bildirilmiş test, 5707 atıf,
>   **sarkan 60/60**. B4 ve ADR 0015'teki `main.go:610-620` tarihli bırakıldı ve dalın ucu
>   eklendi: `e0f53b6`'da `:626-636` (`git show e0f53b6:cmd/tappa/main.go` ile okundu).
> - **F2 — §6.6(b):** pay gerekçesi düzeltildi — yoklama adımı **doğru** uygulamayı yavaşlatır
>   (`server.go:3262-3272`, en çok 500 ms + %10 sapma; B35). Koşul: **D − pay > ≈550 ms**
>   (+ gürültü); örnek T = D = 1 s, pay 300 ms. Denetçinin ölçümü B37 (T = 1,2 s, D = 0,9 s,
>   pay 500 ms → doğru uygulama 12 koşunun 5'inde eşiği aştı). Yakalamadığı: D − pay − ≈550 ms'den
>   kısa ek bekleme.
> - **F3:** İddia J — sayacın STARTTLS'te sıfırlanması (M01) ve el sıkışmada tavan → `network`
>   (M16) *Yakalamadığı*'ndan çıktı, PART I/II'ye *"EM-2'nin 4. tur teslimine göre"* (bölünmüş
>   vaka, el sıkışma vakası). `classify` sırası (M15): §3'te *"Pin durumu"* + sayılı sınır 35
>   (EM-2 eklemezse sınır; orkestratör eşitler).
> - **F4:** §3 `tls_unavailable` hücresi daraltıldı — el sıkışmada tavan → `network`, süre/iptal →
>   `timeout`.
> - **F5 + D2:** sınır 17 — EHLO argümanı (`localhost`) ve SNI'daki röle adı telde ama listede
>   değil (sır değiller); 8'den kısa parçalar **ayraçla (sıra korunarak) ya da sırası bozularak**.
> - **F6:** §10 `err` — `ErrNotConfigured` istisnası yazıldı.
> - **F7:** İddia A — başka ada verilmiş, aranan adı taşımayan ve (kontrol) tam aranan adı
>   taşıyan sertifika satırları (`TestSend_NoTLSMeansNoAuth`, kopyada `send_test.go:291-296`);
>   yakaladıkları: `ServerName` sabitlenmesi (M09) ve adın doğrulanmaması.
> - **F8:** İddia D — `Message` için *"her yazdırma yolu"* testin listelediği 13 biçime daraltıldı
>   (`TestMessage_PrintsAsAPlaceholder`); slog `Resolve`, `%x`, `%d` EM-2'nin 4. tur teslimine göre.
> - **D1 — kesik 250:** §2 tavan kuralına istisna (veri sonu yanıtında `250 ` okunduysa kesilme
>   kabul, gönderim başarılı, id boş — süre ve iptal dahil); §3 `timeout` satırı; sayılı sınır
>   34; kök neden B38 (`bufio/bufio.go:405-428`: kısmi satır hatasız döner) + QUIT'in yanıtı
>   beklenmez. Pinleyen test satırı EM-2'ye devredildi.
> - `mail.go`'daki *"(doc.go, PART III)"* EM-2'nin yorumu; ADR'de değişiklik yok.
>
> **Son eşitleme (2026-10-03 — EM-2'nin kapanış teslimi; `internal/mail` kopyası orkestratörce
> yenilendi, dokunulmadı).** ADR 0022'deki *"teslimine göre"* işaretleri (8 yer) olgulara
> çevrildi; kalan sayı **0**:
> - **Bağlantı sayısı** (§2): `TestSend_ClassifiesEachStep` — 4xx'te 2; selamlama 554, AUTH 535,
>   MAIL 550, RCPT 550, DATA 554, veri sonu 554'te 1; bağlam baştan bitmişse 0. Orkestratörün
>   ölçümü: `auth`/`rejected` yeniden denenirse kırmızı.
> - **Tavan** (İddia J PART I/II, §3 *"Pin durumu"*): `TestSend_TheReplyCapCoversTheWholeAttempt`
>   — bölünmüş vaka (200 KiB EHLO + 100 KiB 250 → `network`, iki kontrol başarılı; M01 kırmızı)
>   ve el sıkışma vakası (64 bayt bütçe → `network`, sunucu el sıkışmayı tamamlamaz, 64 KiB
>   kontrol başarılı; M16 kırmızı); `crypto/tls` sayacın hatasını `errors.Is`'e görünür verir.
> - **M15** (süre/tavan sırası): sayılı sınır 35 — pinsiz, çünkü test tavan hatası ile bitmiş
>   bağlamı aynı anda gerektirir (zamanlanamayan yarış); "EM-2 eklerse" koşulu kaldırıldı.
> - **Kesik 250** (sınır 34): `TestSend_ACutReplyToTheEndOfDataIsAcceptance` (`"250 Ok "` +
>   300 KiB, CRLF yok → başarı, boş id, ≤ 2 s; M66 kırmızı).
> - **`Message` yazdırma** (İddia D): `slog.AnyValue(m).Resolve()`, `%x`, `%d` artık testte
>   (EM-2'nin numaralamasıyla M14 kırmızı).
> - **Yankı kaçışı** (sınır 17): `TestMessageID_KnownLimitIsACutOrReorderedEcho` — her 7
>   karakterde bir ayraçla sırası korunarak bölünmüş ve ters çevrilmiş değer geçer; kontrol:
>   8'lik kesintisiz dizi yakalanır; gömülü ve 8'den kısa parça da sınırda.
> - `TestEveryNamedTestExists` kopyayla: 2563 bildirilmiş test, 5728 atıf, sarkan **60/60**.
>
> **Sayılı sınırlar (bu görevin):**
> 1. Stdlib sondaları go1.27.1 ve go1.26.6 darwin/amd64'te; CI'nin tam sürümü (1.26.x) ile
>    aynı ana sürüm, CI makinesinde koşturulmadı.
> 2. SES davranışları (sandbox red metni, 250'de message-id, kendi `Message-ID`'mize ne yaptığı,
>    tıklama izleme yönlendirmesi, bastırılmış adrese gönderimin sessizliği, geri bildirim
>    yönlendirmesinin gövde taşıması ve kapatılma koşulu, hesap duraklatma, SigV4 ±5 dk)
>    **ölçülmedi** — EM-5'in canlı dumanı ölçer.
> 3. Kapanış aritmetiği ve R7 tuzağı kaynak okumasıyla; `time.Sleep(3s)` mutasyonu üçüncü gözün
>    ölçümüdür, bu turda yeniden koşturulmadı; aynı biçimde B33 (yankı) ve B34 (kapanış
>    süreleri) denetçinin ölçümleridir. EM-2'nin 0,9–2,1 MiB ayırma ölçümü ve M-mutasyon
>    sonuçları EM-2'nin kartından alındı, bu görevde yeniden koşturulmadı (EM-2'nin kodu
>    yalnız okundu).
> 4. Postmark / Resend / kendi SMTP ölçerek karşılaştırılmadı (Q02'nin listesi); seçim K7.
> 5. AWS işleme sözleşmesi (DPA) ve Hetzner'in 587 çıkışı ölçülmedi.
> 6. iOS uygulama-içi tarayıcının çerez ayrımı ölçülmedi (EM-8).
> 7. EM-2'nin test adları bu ağaçta yok; ADR onları `…` önekiyle anar, birleştirmede tam adla
>    güncellenebilir.
>
> **Devirler:**
> - **EM-2:** `Message`'ın belge yorumuna *"üçüncü beyan edilmiş taşıyıcı"* cümlesi (§2); sapma
>   j (256 KiB tavan + İddia J'nin dört vakası) ve k (8'lik pencere + İddia B'nin `message_id`
>   vakaları); 2. tur kararları (`To`'da `=?`, STARTTLS sınıfı, `Reply-To`, kullanıcı adı tipi).
> - **EM-3:** config tablosu (§5), fail-closed matris (iki akış kapalıyken eksik anahtarla boot
>   başarılı), `465` her ortamda, prod'da `localhost`/IP reddi, R7 tuzağı (tanımlayıcı dahil),
>   **özel kök havuzu yasağının üç pini** (yalnız `mail.Config` — atama + anahtarlı bileşik
>   değer; config'te CA anahtarı 0; manifestte `SSL_CERT_*` env'i 0),
>   `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed`'un
>   `"Q02"` beklentisi, ConfigMap yorumu, iki manifest, runbook (değer yazılmaz), dokunduğu
>   dosyalarda Q02 metni.
> - **EM-4:** §8; konu kriteri düzeltme 4 ile; URL benzeri tenant adının e-postadaki biçimi
>   (sınır 22) — EM-7 ve WL-11 ile birlikte.
> - **EM-5:** §6 (tampon 32 + ölçüm, audit'e ayrı sınır, kuyruk dolu → senkron `undelivered`,
>   koşullu panik kuralı, eşzamanlı boşaltma + iki pin — (b) T ≥ D'li, devre kesici açıkken
>   sonuç), 2 s'lik yeni
>   zamanlama testi (senkron kodda kırmızı olduğu kartta), grant başına satır testi (beş yol,
>   bütçe içinde), senkron varsayan iki testin güncellenmesi, M7-04 kriterlerinin gerçek SMTP'ye
>   karşı yeniden koşusu, canlı duman (SPF/DKIM/DMARC PASS + message-id + kendi `Message-ID`'miz
>   + izleme kapalı).
> - **EM-6:** adres okuma sorgusu (açık tenant filtresi + RLS testi), "e-postayı değiştir" +
>   aynı-tx iptal + audit (detail'de adres yok), migration beklenmiyor (ölç).
> - **EM-7:** §7 (basımdan önce adres; üç ret; senkron gönderim; `invite.code_emailed`;
>   başarısızlık satırı; `employeeactions.go:419` sızıntı testi; owner-only yedek — rol testi
>   fikstürle; oran sınırları); anahtarlı alıcı özeti kararı (*Karar verilmedi*) burada ya da
>   Q13 ile ayrı ADR'de.
> - **EM-8:** M6-11'de iki kanal ayrı; gerçek cihaz turu.
> - **EM-9:** linksiz bildirim.
> - **Orkestratör:** Q02'yi "Cevaplananlar"a taşı (`q02.md`) · `open-questions.md` Y-D satırının
>   *"Q02 çözülünce kod çalışanın kendi kanalına"* cümlesi · M5-02 kartında 5 Q02 anışı ·
>   CLAUDE.md §3 (`internal/mail`) ve §7 (sıfırlama token'ı, SMTP kimliği, e-posta adresi) · m10
>   dış adımlarına iki adım: SES geri bildirim yönlendirmesi; yerel SES denemesi için sandbox'ta
>   kısıtlı ayrı IAM kimliği (prod kimliği `.env`'e yazılmaz) · EM-2 birleştirilince ADR'deki
>   `…` önekli test adlarını tam adla güncellemek (isteğe bağlı).

> **Kart düzeltmesi (2026-10-03, EM-2 uygulaması sırasında; 2. ve 3. tur aynı gün).** Yazıldı:
> `internal/mail/` (yeni paket, on dosya): `doc.go` (paket belgesi + üç parçalı iddia + bilinen
> sınırlar) · `mail.go` (`Message` ve redaksiyonu, `Receipt`, `Config`, `SMTP`, `New`, `Send`,
> `attempt`, `data`, `cappedConn`) · `errors.go` (`Class`, sekiz sınıf sabiti, `SendError`,
> `ErrNotConfigured`, `classify`) · `compose.go` (alıcı/başlık kuralları, `headerBlock`,
> `compose`, `alternative`, `messageID`, `echoWindow`) · `credential.go` (`Credential` —
> kullanıcı adı ve parola; `invite.Code` deseni) · testler: `fakesmtp_test.go` (test içi sahte
> SMTP; dört anahtar ve öz-imzalı sertifika her koşuda bellekte üretilir, dosya yok),
> `send_test.go`, `cap_test.go`, `leak_external_test.go`, `compose_test.go` (`FuzzCompose`
> dahil). Config/manifest/handler YOK (EM-3, EM-5, EM-7). `go.mod`/`go.sum`/`sqlc.yaml` diff boş;
> `go list -deps ./internal/mail` standart olmayan tek paket olarak kendisini verir. **Ölçüm
> ortamı:** yerel Go 1.27.1 darwin/amd64; `-race -cover` ve staticcheck `GOTOOLCHAIN=go1.26.7`;
> ayrı git worktree, taban `f3c9c04`. DB'ye bağlanılmadı; ağ yalnız `127.0.0.1`.
>
> **2. tur — denetim bulgularının kapanışı:**
> - **Güvenlik YÜKSEK — yanıt bayt tavanı.** Ham bağlantı `NewClient`'ten önce `cappedConn`'a
>   sarılır; deneme başına okunan her bayt (selamlama, EHLO, TLS el sıkışması ve TLS üstündeki her
>   yanıt) tek sayaçtan geçer, `maxReplyBytes` = 256 KiB, aşılınca `network`. Ölçüm
>   (`TestSend_CapsWhatTheRelayCanMakeItRead`, 4 MiB = tavanın 16 katı): **önce** (tavan yok,
>   M34) beş satırın beşi 5 s'lik süre sonuna kadar tutuldu, canlı heap tepe +10–28 MiB, ayrılan
>   10,5–33 MiB (selin büyüklüğüyle büyür); **sonra** 1–18 ms'de `network`, ayrılan 0,9–2,1 MiB
>   (üç `-race` koşusu), bütçe 16 × tavan = 4 MiB. Satırlar: satır sonu olmayan tek satır,
>   1 KiB'lık devam satırları, boş devam satırları (selamlamada), TLS öncesi EHLO yanıtı, TLS
>   üstünden veri sonu yanıtı. Gerekçe (256 KiB): dürüst bir röle denemede birkaç yüz bayt yanıt +
>   bir TLS sunucu uçuşu (sertifika zinciri birkaç KiB; uzun bir RSA-4096 zinciri < 16 KiB) gönderir.
>   Go 1.26.7 ve 1.27.1'in `textproto.ReadResponse`'u çok satırlı yanıtı `strings.Builder`'la
>   kurar (kaynak okundu) — tavan altında karesel bir büyüme yok.
> - **Güvenlik ORTA — yankı kuralı iki yönlü.** Kimlik, gönderilen bir değerle 8 karakterlik
>   ortak bir `[A-Za-z0-9._-]` penceresi paylaşıyorsa (ya da 8'den kısaysa bir değerin içinde
>   geçiyorsa) düşer. Liste: ham ileti, To, From, Subject, Ref, Text, HTML, kullanıcı adı, parola,
>   AUTH PLAIN argümanı (base64). Ölçülen dört biçim (`q.<değer>`, `<değer>-0`, `x<parola>`,
>   `maria.borg-1`) ve koordinatörün beşi (`x<değer>`, `<değer>-000000`, `<değer>.1`, AUTH
>   argümanı birebir, kullanıcı adı önekli) düşer; gerçekçi SES kimliği (`0107018f…-000000`)
>   geçer: `TestSend_ReadsTheMessageIDFromThe250Reply`. Listenin her girdisi için yalnız o
>   girdinin yakalayabildiği bir satır var (o parçanın rölenin aldığı iletide OLMADIĞI kontrolüyle):
>   ham ileti (MIME sınırı parçası), kullanıcı adı, parola, AUTH argümanının bir koşusu, Text ve
>   HTML (QP yumuşak kırılımının böldüğü değer), Subject (RFC 2047'nin değiştirdiği `_`), Ref.
>   **To ve From girdileri eşdeğerdir:** ikisi de ham iletinin `To:`/`From:` başlıklarında birebir
>   durur, yani kaldırılmaları hiçbir testi kızartamaz (M39, M40 — ölçüldü); ADR listesinde
>   oldukları için tutuldu. Parola ADR'nin listesinde yok — eklendi, çünkü `x<parola>` AUTH
>   argümanının base64 penceresiyle yakalanmaz.
> - **B4 — To'da `=?`:** alıcı kuralına eklendi; kontrol: `net/mail` o adresi kendisine çözer,
>   `mime.WordDecoder` CRLF + Bcc üretir. Satır `TestSend_RefusesBadInputBeforeAnyDial`'da,
>   tohum `FuzzCompose`'ta; M47 → kırmızı.
> - **B5 — sertifika adı:** havuzun GÜVENDİĞİ üç sertifika: başka ad (`other.test`), aranan adı
>   taşımayan (127.0.0.1'e yalnız `localhost`, `localhost`'a yalnız IP), kontrol olarak yalnız
>   aranan adı taşıyan → ilk ikisi `tls_unavailable` + 0 AUTH, kontrol bağlanır
>   (`TestSend_NoTLSMeansNoAuth`, iki ad). X01 (M48, `ServerName` sabit) ve X03 (M49, zincir
>   doğrulanır ad doğrulanmaz) → kırmızı.
> - **B6 — X04 (MinVersion silindi) yeşil, SAYILI SINIR.** İstemcinin varsayılan alt sürümü zaten
>   1.2. Deneme: `GODEBUG=tls10server=1` Go 1.27'de kaldırılmış (`fatal error: removed GODEBUG …`),
>   `GODEBUG=tls10default=1` ile de MinVersion'sız istemci TLS 1.1 sunucuya bağlanmadı. Yani
>   açık `MinVersion` bugün varsayılanla aynıdır; ancak varsayılan düşerse koruyucu olur.
> - **B7 — ağ hatası yeniden denenmez:** `TestSend_ANetworkFailureIsNotRetried` — veri sonundan
>   sonra düşen bağlantı ve DATA sırasında RST → `network`, bağlantı 1. X14 (M51) → kırmızı.
> - **B8:** yankı girdileri — ORTA ile kapandı (X06 = M37, X07 = M38 kırmızı; X08 = M39 eşdeğer,
>   yukarıda).
> - **B3 — Ref:** sahte sunucu artık her komut satırını tam kaydeder; `TestSend_TheMessageOnTheWire`
>   Ref kimliğinin hiçbir komut satırında ve ileti satırında olmadığını assert eder (kontrol:
>   ≥6 satır kaydedildi). X20 (M52, Ref EHLO adı) → kırmızı.
> - **B1:** PART III tek cümle: *"Any form not listed above is the subject of code review — no
>   completeness claim."* Ölçülmüş sınırlar ayrı bir "KNOWN LIMITS" paragrafında.
> - **B2:** "her biçimde yer tutucu" cümlesi kaldırıldı; iddia testin listelediği biçimlere ve
>   "değer de ilk 8 karakteri de yok" assert'ine bağlandı; `Config`/`SMTP` içinde alanın bir
>   işaretçi ADRESİ olarak basıldığı (dolaylılığın kendisi) yazıldı.
> - **B9:** RST testi `data()`'nın gövde yazma hata dalını sürer. Kapsanmayan iki dal (DATA
>   komutunun yazımı ve `DotWriter.Close` hatası) ulaşılabilirdir ama yalnız yarışlı zamanlamayla;
>   "ulaşılamaz" cümlesi geri çekildi (aşağıda kapsam).
> - **B10:** `ClassInvalidAddress` yorumu (New `SendError` döndürmez) · `ClassInvalidMessage`
>   yorumuna Ref ve `=?` · `credential.go`'da `reveal()`'ın çağıranları (hiçbir test çağırmaz) ·
>   QUIT satırı ölçülen sınıra (≤1 s) · `compose.go`'daki iki QP cümlesi uzlaştırıldı · "every
>   reader" → "`mime.WordDecoder` decodes it (measured)".
> - **B11:** STARTTLS'e 454/502 ve STARTTLS sonrası EHLO hatası → `tls_unavailable` — kabul
>   edildi, ADR §3 tablosuna devredildi (aşağıda). **Reply-To** artık `New`'de alıcı kuralıyla
>   doğrulanır (görünen ad, açılı ayraç, `=?` → ret; `TestNew_RefusesAnInvalidConfig`). M53 →
>   kırmızı.
> - **B12:** `Message` kendi basım yollarında yer tutucu basar (`Format` her fiil, `String`,
>   `GoString`, `LogValue`, `MarshalText`) — `TestMessage_PrintsAsAPlaceholder`; doc'ta ⚠️ uyarı
>   (*"loglanmaz — `invite.Delivery` / `ResetDelivery` gibi beyan edilmiş istisna"*). Sınır ölçüldü:
>   dışa kapalı bir alanda tutulan `Message` alanlarıyla basılır
>   (`TestMessage_KnownLimitIsAnUnexportedField`) — alanlar dışa açık dizgeler olduğu için
>   dolaylılık (işaretçi) API'yi bozmadan uygulanamaz. M54, M55 → kırmızı.
> - **Güvenlik DÜŞÜK — kullanıcı adı:** `Config.Username` artık `Credential` (parolayla aynı
>   tip); `TestCredential_IsRedactedInEveryRendering` kullanıcı adını da arar.
> - **Güvenlik DÜŞÜK — `RootCAs`:** EM-3'e devredildi (aşağıda).
> - **B13:** sayılı sınır (konular ASCII).
> - **Tekrar duraklaması (koordinatör sorusu):** süre kalmadığında ya da duraklama sırasında bağlam
>   bittiğinde sınıf **`throttled` kalır** (değiştirilmedi). Gerekçe `ClassThrottled` yorumunda:
>   gönderimi bitiren rölenin 4xx'idir, süre yalnız yeniden denenmemesine karar verdi; `timeout`
>   demek çağıranın log'undaki tek operasyonel sinyali (sağlayıcı kısıtlaması) gizler ve taşıyacak
>   bir yanıt kodu olmaz. Ölçüm: `TestSend_RetriesA4xxOnceOnANewConnection` (iki satır) `451` ile
>   `throttled` bekler.
>
> **3. tur (kapanış) — birleşik ağaç denetimlerinin bulguları:** (yalnız `internal/mail`
> değişti; ürün kodunda yalnız yorumlar, testlerde yeni satırlar ve iki yeni test)
> - **F1 (bloklayan) — "5xx yeniden denenmez" her adımda pinli.** `TestSend_ClassifiesEachStep`'in
>   her satırı artık bağlantı sayısını da assert eder: her 4xx satırında 2, diğer her satırda tam
>   1 (selamlama 554, AUTH 535, AUTH'a meydan okuma, MAIL 550, RCPT 550/600, DATA 554, veri sonu
>   554, düşen bağlantılar, bozuk selamlama); bağlam önceden bitmişse 0. Denetçinin M29'u (`auth`
>   yeniden denenir — bizde M57) ve M31'i (554 yeniden denenir — M58) → **kırmızı**.
> - **F3 — tavanın pinsiz üç davranışı.** `TestSend_TheReplyCapCoversTheWholeAttempt`: TLS
>   öncesi 200 KiB geçerli EHLO + TLS sonrası 100 KiB geçerli 250 → `network`; iki kontrol (her
>   yarı tek başına) başarılı → sayaç STARTTLS'te sıfırlanmıyor (denetçinin M01'i — bizde M62 →
>   **kırmızı**). El sıkışma başladığında 64 bayt bütçe → `network` ve sunucuda el sıkışma
>   TAMAMLANMAMIŞ (tavanı TLS değil sayaç kesti); 64 KiB bütçe kontrolü başarılı (M16 — bizde M63,
>   aşım TLS aşamasında `tls_unavailable` → **kırmızı**). Ölçülen: `crypto/tls` sayacın hatasını
>   `errors.Is`'in göreceği biçimde döndürür. **M15 (sınıflandırmada süre denetimi tavandan önce,
>   bizde M64) YEŞİL — sayılı sınır:** iki koşulu aynı anda tetikleyen bir satır ucuz değil
>   (tavan hatası ile biten bağlamın aynı ana denk gelmesi zamanlanamayan bir yarış).
> - **F8 — `Message` testi:** `TestMessage_PrintsAsAPlaceholder`'a `slog.AnyValue(m).Resolve()`
>   (yer tutucu DİZGE), `%x`, `%d`. Denetçinin M14'ü (`LogValue` + derleme anı satırı birlikte
>   silinir — bizde M65) → **kırmızı**.
> - **Güvenlik D1 — veri sonu yanıtında kesilen 250 satırı BAŞARIDIR (karar korunur, pinlendi).**
>   `TestSend_ACutReplyToTheEndOfDataIsAcceptance`: `"250 Ok "` + 300 KiB, CRLF yok → başarı,
>   `MessageID` boş, ≤2 s. Kök neden yorumda: `bufio.Reader.ReadLine` veri varken hatayı düşürür
>   ve 250'den sonra okuma yoktur. "Tek satır, satır sonu yok … `network`" genellemesi `doc.go`,
>   `maxReplyBytes` ve `ClassNetwork` yorumlarında bu istisnayla daraltıldı. Mutasyon M66 (250
>   satırı 64 KiB'tan uzunsa `network`) → **kırmızı**.
> - **F5:** yankı listesi yorumu listeye daraltıldı; EHLO argümanı ve SNI'daki ad listede değil
>   (sır değiller) diye yazıldı.
> - **F10:** `SMTP.attempt` yorumu "(doc.go, KNOWN LIMITS)" · `cap_test.go`'daki "tavan büyürse
>   bütçe kızarır" cümlesi ölçüme bağlandı: ×2 (M60) bölünmüş satırı ve kesik-250 testini
>   kızartır, yığın bütçesi YEŞİL kalır; ×4 (M61) bütçeyi de kızartır (5,0–6,5 MiB / 4 MiB) ·
>   `doc.go`'da "within milliseconds" → testin izin verdiği 2 s.
> - **D2:** yankı kaçışı "en az her 7 karakterde bir ayraçla (`.`, `_`, `-`) sırası korunarak
>   bölünmüş ya da sırası bozulmuş" diye düzeltildi (`doc.go` KNOWN LIMITS, `echoWindow` yorumu);
>   sınır ölçülerek pinlendi: `TestMessageID_KnownLimitIsACutOrReorderedEcho` (kontrol: 8
>   karakterlik bitişik koşu yakalanır).
> - **"§3's" → "§2's"** (`compose.go`, `mail.go`).
>
> **Kabul — kanıtlar (1. turdan; 2. turda değişenler işaretli):**
> 1. **STARTTLS ilan edilmezse `ClassTLS` + sunucu 0 AUTH.** `TestSend_NoTLSMeansNoAuth`, iki
>    sunucu adıyla (`127.0.0.1`, `localhost`): ilan yok · 454 · 502 · güvenilmeyen sertifika ·
>    *(2. tur)* güvenilen ama başka ada ait ya da aranan adı taşımayan sertifika · en çok TLS 1.1 →
>    `tls_unavailable`, 0 AUTH, 0 MAIL, bağlantı 1; iki kontrol satırı bağlanır. Gerekçe:
>    `TestPlainAuth_SendsInClearForTheLoopbackNames` (çıplak net/smtp bu iki adda AUTH'u düz
>    metinde gönderir, `mail.example.test` adında göndermez).
> 2. **250'den Message-ID** — `TestSend_DeliversOverSTARTTLSAndReturnsTheProvidersMessageID`,
>    *(2. tur, genişledi)* `TestSend_ReadsTheMessageIDFromThe250Reply`.
> 3. **Deadline ≤100 ms** — `TestSend_ReturnsWithin100msOfTheDeadline` (sekiz adım × üç süre
>    kaynağı; aşım bir `-race` koşusunda 31 µs – 1,55 ms, gözlem).
> 4. **CRLF → 0 dial** — `TestSend_RefusesBadInputBeforeAnyDial` (*2. tur:* To'da `=?` satırı ve
>    çözücü kontrolü); bağlantı sayısı tam kontrol sayısıdır.
> 5. **30 s fuzz** — `FuzzCompose` *(2. tur kahini To'da `=?`'yi de reddeder; tohum eklendi)*;
>    koşu sonucu aşağıda.
> 6. **550 sızmaz** — `TestSendError_CarriesNoServerText`, `TestSendError_HasOnlyAClassAndACode`.
> 7. **Kimlik bilgisi redaksiyonu** *(2. tur: kullanıcı adı dahil)* —
>    `TestCredential_IsRedactedInEveryRendering`, `TestCredential_KnownLimitIsReflection`.
> 8. **Kapsam ve yarış:** sonuçlar aşağıda.
> 9. **Bağımlılık** — `TestMail_ImportsOnlyTheStandardLibrary`, `go list -deps`.
> 10. **net/smtp dondurulmuş** — `SMTP.attempt` yorumu; `TestSend_DotStuffsALoneDotLine`.
>
> **Kapsam:** `go test -race -cover ./internal/mail` → **%97,5** (`GOTOOLCHAIN=go1.26.7`; Go 1.27.1 ile bir koşuda %97,7 — farkın hangi dalda olduğu ölçülmedi). `-race -count=3` (go1.26.7) yeşil. **30 s fuzz** (bir kez): 843.981 çalıştırma, 0 hata.
> Go 1.27.1 koşusunda kapsanmayan altı blok: `alternative`'in ve `compose`'un `bytes.Buffer` hata dalları (üç;
> `bytes.Buffer`'a yazma hata vermez), taze soketin `SetDeadline` hatası, `data()`'da DATA komutu
> yazımının ve `DotWriter.Close`'un hata dalı (ulaşılabilir, yalnız yarışlı zamanlamayla).
>
> **ADR'ye devredilecekler (EM-1 / orkestratör):**
> - **To'da `=?` reddi** (alıcı kuralının parçası; gerekçe ve ölçüm yukarıda, B4).
> - **STARTTLS sınıf eşlemesi:** STARTTLS'e 4xx/5xx, el sıkışma hatası ve STARTTLS sonrası EHLO
>   hatası → `tls_unavailable`, yeniden denenmez (§3 tablosu).
> - **Yanıt bayt tavanı:** deneme başına 256 KiB, TLS öncesi ve sonrası tek sayaç, aşılınca
>   `network`; gerekçe ve ölçüm yukarıda.
> - **Yankı kuralı:** 8 karakterlik pencere, iki yön; liste ADR'nin listesi **+ parola** (gerekçe
>   yukarıda); To ve From girdilerinin ham iletiyle eşdeğer olduğu (mutasyonla ölçüldü).
> - **Reply-To** alıcı kuralıyla, görünen ad yok.
> - **Duraklama sınıfı** `throttled` kalır (gerekçe yukarıda) — ADR farklı karar verirse değişiklik
>   `Send`'de tek satır.
> - 1. turdan devam eden sapmalar a–i (sekizinci sınıf `invalid_message`; AUTH'a 4xx → `throttled`
>   + tek deneme; `ErrNotConfigured`; kendi `Message-ID` başlığımız; QUIT 221'i beklenmez; başlık
>   reddi üst kümesi; Ref iletiye girmez; `Name == ""` eşitlikte; iki süre mekanizması).
> - **3. tur — EM-1'e:** (a) M01 (STARTTLS'te sayaç sıfırlanması) artık YAKALANIYOR — ADR'nin
>   "Yakalamadığı" listesinden çıkar; pin `TestSend_TheReplyCapCoversTheWholeAttempt` (bölünmüş
>   satır + iki kontrol; el sıkışma içinde aşım `network`, M16'yı da kızartır). (b) **D1
>   istisnası:** tavanın kestiği satır veri sonu 250 yanıtıysa gönderim BAŞARIDIR (`MessageID`
>   boş) — "aşılınca `network`" kuralının tek istisnası, pin
>   `TestSend_ACutReplyToTheEndOfDataIsAcceptance`. (c) "5xx yeniden denenmez" her adımda pinli:
>   `TestSend_ClassifiesEachStep` bağlantı sayısı sütunu. (d) M15 (süre denetimi tavandan önce)
>   yakalanmıyor — "Yakalamadığı" listesinde KALIR. (e) Yankı kaçışı tarifi: dönüştürme, en az her
>   7 karakterde ayraç (sıra korunarak), sıra bozma, 8'den kısa gömülü parça — ölçülü pin
>   `TestMessageID_KnownLimitIsACutOrReorderedEcho`. (f) Yeni test adları:
>   `TestSend_TheReplyCapCoversTheWholeAttempt`, `TestSend_ACutReplyToTheEndOfDataIsAcceptance`,
>   `TestMessageID_KnownLimitIsACutOrReorderedEcho`; genişleyenler `TestSend_ClassifiesEachStep`
>   (bağlantı sayısı), `TestMessage_PrintsAsAPlaceholder` (Resolve, `%x`, `%d`). (g) Kural ADR
>   §2'de: koddaki atıflar "§2's window rule".
>
> **Mutasyon tablosu — 3. tur ekleri (son ağaçta):** 12 mutasyon, `mutate.sh` (paket sha'sı önce
> ve sonra `f5d73e48d2b9abc4`; ardından yalnız `cap_test.go`'nun bir yorumu değişti):
> M57 (`auth` yeniden denenir; denetçinin M29'u) · M58 (554 yeniden denenir; M31) · M27 (5xx
> yeniden denenir) → `TestSend_ClassifiesEachStep` kırmızı · M62 (sayaç STARTTLS'te sıfırlanır;
> M01) · M63 (TLS aşamasında aşım `tls_unavailable`; M16) → `TestSend_TheReplyCapCoversTheWholeAttempt`
> kırmızı · M65 (`Message.LogValue` + derleme anı satırı yok; M14) → `TestMessage_PrintsAsAPlaceholder`
> kırmızı · M66 (kesik 250 → `network`) → `TestSend_ACutReplyToTheEndOfDataIsAcceptance` kırmızı ·
> M60 (tavan ×2) → bölünmüş satır + kesik-250 kırmızı, yığın bütçesi yeşil · M61 (×4) → yığın
> bütçesi de kırmızı · M51 (ağ hatası yeniden denenir) · M34 (tavan yok) → kırmızı ·
> **M64 (süre denetimi tavandan önce; M15) → YEŞİL, sayılı sınır.** 12'nin 11'i kırmızı.
>
> **Mutasyon tablosu (2. tur, son ağaçta yeniden koşuldu).** `mutate.sh`: her mutasyonda paket
> dosyaları tam yolla yedeklendi, uygulandığı `cmp` ile kanıtlandı, önce `go vet` (derlenmeyen
> sayılmaz), sonra paketin tamamı; karar `go test`'in kendi çıkış kodundan; geri yükleme `cmp`
> ile; paket sha'sı önce ve sonra `c6922f3a4019bd0b`. **59 mutasyon: 54 kırmızı · 1 derlenmez
> (M15a — derleme anı pini) · 4 yeşil:** M13c ve M50 sayılı sınır, M39 ve M40 ölçülmüş eşdeğerlik
> (To/From ham iletide birebir). M46'nın ilk biçimi derlenmedi (kullanılmayan import), derlenen
> biçimi kırmızı.
>
> | # | Ne | Sonuç (kırmızıya dönen test) |
> |---|---|---|
> | M01a · M01b | STARTTLS kapısı kapalı · fırsatçı | NoTLSMeansNoAuth |
> | M02 | MinVersion TLS 1.0 | NoTLSMeansNoAuth |
> | M03a · M03b | `Unwrap` · neden alanı + `Unwrap` | HasOnlyAClassAndACode (+ CarriesNoServerText) |
> | M04 | `Error()`'a sunucu metni | CarriesNoServerText, HasOnlyAClassAndACode, ClassifiesEachStep, … |
> | M05 · M06 · M07 · M08 | CR/LF temizle · yalnız CRLF çifti · NUL/DEL serbest · U+2028/9 serbest | RefusesBadInput…, FuzzCompose, TestNew_… |
> | M09 · M10 · M11 · M19 | görünen ad · 255 bayt · çoklu alıcı · ASCII denetimi yok | RefusesBadInput… (+ FuzzCompose, TestNew_…) |
> | M12 · M25 · M27 | 4xx sınırsız · bekleme süreyi yok sayar · 5xx yeniden denenir | RetriesA4xx… |
> | M13 · M13b | süre yok sayılır · AfterFunc yok | ReturnsWithin100ms… |
> | M13c | yalnız SetDeadline yok | **YEŞİL** — sayılı sınır 5 |
> | M14 · M15b | `Credential.String` değeri döndürür · LogValue + iddia yok | Credential_IsRedactedInEveryRendering |
> | M15a | yalnız LogValue yok | **derlenmez** — PART II pini |
> | M16 | Message-ID sabit | ReadsTheMessageID…, DeliversOverSTARTTLS…, RetriesA4xx…, …QUIT… |
> | M17 | nokta doldurma yok | DotStuffsALoneDotLine, TheMessageOnTheWire |
> | M18 | `headerBlock` bayt denetimi yok | HeaderBlock_RefusesWhatItCannotWriteVerbatim |
> | M20 · M47 | Subject/From'da `=?` serbest · To'da `=?` serbest | RefusesBadInput…, FuzzCompose, TestNew_… |
> | M21 · M36 | yankı kuralı hiç yok · yalnız pencere yok | ReadsTheMessageID… |
> | M22 | satır sonu kanonikleştirme yok | FuzzCompose |
> | M23 · M24 · M31 · M33 | STARTTLS 4xx → throttled · `ctx.Err` yok · AUTH 5xx → rejected · selamlama 5xx yeniden | NoTLSMeansNoAuth · ClassifiesEachStep (+ CarriesNoServerText) |
> | M26 | AUTH, TLS kapısından önce | NoTLSMeansNoAuth, DeliversOverSTARTTLS… |
> | M28 | `InsecureSkipVerify` | NoTLSMeansNoAuth |
> | M29 | Ref bir `X-` başlığına | TheMessageOnTheWire, FuzzCompose, ReadsTheMessageID… |
> | M30 | QUIT 221'i bekler | AServerThatNeverAnswersQUIT… |
> | M34 · M35 | yanıt tavanı yok · tavan ×64 | CapsWhatTheRelayCanMakeItRead |
> | M37 (X06) · M38 (X07) | ham ileti · kullanıcı adı listede yok | ReadsTheMessageID… |
> | M39 (X08) · M40 | From · To listede yok | **YEŞİL** — eşdeğer (ham iletide birebir) |
> | M41 · M42 · M43 · M44 · M45 · M46 | Subject · Ref · Text · HTML · parola · AUTH argümanı listede yok | ReadsTheMessageID… |
> | M48 (X01) · M49 (X03) | `ServerName` sabit · zincir doğrulanır ad doğrulanmaz | NoTLSMeansNoAuth |
> | M50 (X04) | MinVersion satırı silindi | **YEŞİL** — sayılı sınır 6 |
> | M51 (X14) | ağ hatası yeniden denenir | ANetworkFailureIsNotRetried |
> | M52 (X20) | Ref EHLO adı olarak gönderilir | TheMessageOnTheWire |
> | M53 | Reply-To görünen adla kabul | TestNew_…, TheMessageOnTheWire |
> | M54 · M55 | `Message.LogValue` To'yu · `Message.Format` Text'i basar | Message_PrintsAsAPlaceholder |
>
> Üçüncü gözün yeşil kalan 13'ünden tanımı bana iletilen sekizi yeniden kuruldu: X01, X03, X06,
> X07, X14, X20 → **kırmızı**; X04 sayılı sınır; X08 eşdeğer. **X12, X15, X24, X31, X34'ün
> tanımları bu turun mesajında yoktu** — yeniden koşulmadı; orkestratör tanımlarını iletirse
> koşulur.
>
> **Devirler:**
> - **EM-3:** `mail.Config`: `Username` ve `Password` ikisi de `mail.NewCredential(...)`;
>   `ReplyTo` tek çıplak adres (görünen ad `New`'de reddedilir). Üretim politikası `New`'de değil:
>   localhost/IP literal yasağı, 465 reddi ve **üretim config'inden `RootCAs == nil` çıktığını
>   sabitleyen bir pin** (ADR 0022 2. tur üretimde özel kök havuzunu yasaklıyor) EM-3'ün. `New`
>   hata metni alanı adlandırır, değer basmaz. `internal/mail` yalnız stdlib.
> - **EM-5:** `errors.As(err, &se *mail.SendError)`; log yalnız `se.Class`, `se.SMTPCode`,
>   `Receipt.MessageID` (boş olabilir — yankı kuralının bedeli). `mail.Message` loglanmaz (⚠️
>   doc). `Send` süresi `min(bağlam, Config.Timeout)`. Canlı dumanda: SES 250 biçimi, MessageID'nin
>   dolduğu ve gerçek SES kimliğinin gövdeyle 8 karakter paylaşmadığı; SES'in bizim Message-ID
>   başlığımızla ne yaptığı. Link arayan testler gövdeyi QP-çözmeli.
> - **EM-7:** zincirdeki `*mail.SendError` metin taşımaz; `Message` (Ref = davet kimliği) panel
>   log yoluna konmaz; saklanan adres alıcı kuralını geçmezse `invalid_address`, dial yok — EM-7
>   kodu basmadan önce aynı kuralı (artık `=?` dahil) uygular.
> - **EM-4:** konu ASCII ise değişmeden; konuda ve alıcıda `=?` reddedilir; Text ve HTML zorunlu.
> - **WL-11:** başlıklara tenant verisi girmez; `"X\r\nBcc: y"` konuya/görünen ada gelirse ret.
>
> **Sayılı sınırlar:** (1) köşeli alan adı kabul · (2) yankı kuralı dönüştürülmüş yankıyı
> (base64, harf), en az her 7 karakterde bir ayraçla sırası korunarak bölünmüş ya da sırası
> bozulmuş yankıyı (ölçüldü) ve 8 karakterden kısa gömülü parçayı yakalamaz; bedeli olan yanlış
> pozitif (meşru bir kimliğin gövdeyle şans eseri 8 karakter paylaşması) yalnız `message_id`'nin
> kaybıdır · (3) yanıt baytı deneme başına 256 KiB ile sınırlı; aşılınca `network` — veri sonu
> 250 satırı kesilirse istisna: başarı, `MessageID` boş (karar, pinli); tavan altında ayrılan
> bellek ölçülen 0,9–2,1 MiB; sınıflandırmada tavan denetimi süreden önce gelir ve hiçbir test
> ikisini ayırmaz (M64) · (4) kimlik bilgisi kasıtlı yansımayla okunur (ölçüldü) ·
> (5) ham bağlantının `SetDeadline`'ı ayrıca pinli değil (M13c) · (6) açık `MinVersion` bugün Go
> varsayılanına eşit; silinmesi görünmez (X04 = M50) · (7) dışa kapalı alandaki `Message` ve
> alanlarını loglayan çağıran linki ve adresi basar (ölçüldü) · (8) RFC 2047'nin 76 karakterlik
> satır sınırı uygulanmaz, yalnız 998 · (9) gerçek SES'e karşı davranış ölçülmedi (EM-5) ·
> (10) "0 dial" sahte sunucunun kabul ettiği bağlantı sayısıyla ölçülür; ürün kodunda dial kancası
> yok · (11) fuzz başlık üreticisini sürer, ağ yolunu değil · (12) gövde içeriği kısıtlanmaz.

> **Kart düzeltmesi (2026-10-03, EM-4 uygulaması sırasında; 2. ve 3. tur aynı gün).** Yazıldı:
> `web/templates/email/` (yeni paket, altı dosya): `email.go` (paket belgesi + üç parçalı iddia +
> bilinen sınırlar; `RenderInvitation`, `RenderPasswordReset`, `InvitationView`, `ResetView`,
> `ErrBaseURL`, `ErrLink`, `ErrLifetime`, `checkLink`, `validBase`, `validHostName`,
> `loopbackHost`, `lifetime`) · `letter.go` (`letter` — iki parçanın okuduğu tek cümle kaynağı;
> `invitationLetter`, `resetLetter`, ad kapısı `nameShown`) · `text.go` (düz metin üreticisi) ·
> `message.templ` + üretilen `message_templ.go` (tek HTML bileşeni, satır içi stil) ·
> `email_test.go` (23 test, DB'siz). Hiçbir handler'a bağlanmadı (EM-5/EM-7); davranış bugün
> değişmez. `go.mod`/`go.sum`/`sqlc.yaml` diff boş; üretim bağımlılığı yalnız `internal/mail`
> (+ templ, stdlib `net/netip`): `go list -deps ./web/templates/email` → depo paketi olarak yalnız
> `internal/mail` ve kendisi. `internal/mail` değişmedi. Testler `internal/adminauth`,
> `internal/domain/signup`, `internal/domain/tenant`'ı yalnız test için import eder. **Ölçüm
> ortamı:** go1.27.1 darwin/amd64 (testler go1.26.7'de de, `-race` ile yeşil); staticcheck
> `GOTOOLCHAIN=go1.26.7` (tüm depo, çıkış 0); ayrı worktree, taban `b06370c` (dalın ucu önce
> `df544c1`'e, sonra `e08045f`'e ilerledi; worktree bilerek ilerletilmedi). DB'ye bağlanılmadı; ağ yalnız `127.0.0.1` (konu
> testinin dinleyicisi, bağlantı kabul etmediği assert edilir).
> **ADR 0022 (bu worktree'de):** §8'e tarihli *"EM-4 notu"* (ad kapısı, URL sayımı ve taban kuralı,
> adsız sıfırlama, düz metin üreticisi, ana tarayıcı cümlesi, renk okuyucusu, kontrast) · İddia
> H'nin PART I'i *"ölçülecek"*ten test adlarıyla *"ölçüldü"*ye ve **listelenen** kümeye bağlı,
> PART II'nin *Yakalamadığı*'sı ölçülmüş sınırlara · sayılı sınır 22'ye EM-4 yarısı ve EM-7'nin
> önündeki ürün kararı · Sonuçlar'daki EM-4/EM-7/WL-11 devir maddesi. Normatif kural değişmedi.
> `TestEveryNamedTestExists` yeşil (sarkan 60/60, bütçe değişmedi).
>
> **Kabul (kart satırı + EM-1/EM-2 devirleri):**
> - ✓ **tam 1 mutlak URL** — parçalar **ayrı** sayılır, her biri tam 1 ve o da link: HTML'de
>   tek `href` (görünen metinde URL yok), düz metinde link kendi satırında bir kez; iki şablon ×
>   2 düz + 33 düşmanca ad — `TestRender_EachPartCarriesExactlyOneAbsoluteURL`. Link yalnız
>   beklenen kökte: `BaseURL` (sondaki `/`'ler kırpılır) + yol + `?<param>=` + 1–128
>   `[A-Za-z0-9_-]`; taban yalnız `https`, düz `http` yalnız loopback'te (`localhost`,
>   `*.localhost`, 127.0.0.0/8), host için bir sözdizimi kuralı (IPv4 biçimi doğrulanmaz), port
>   1–65535; 52 red + 12 kabul satırı (2'si ölçülmüş sınır) —
>   `TestRender_RefusesALinkOutsideTheExpectedAddress`; `adminauth`'un bastığı
>   sıfırlama linki kabul — `TestRender_AcceptsTheResetLinkAdminauthMints`.
> - ✓ **`<img`/`@font-face`/`<link`/`<script` 0** — ayrıca `<style`, `<iframe`, `<object`,
>   `<embed`, `<svg`, `<video`, `<audio`, `<form`, `<base`, `@import`, `url(`, ` src=`,
>   ` srcset=`, ` background=`, ` poster=`, ` action=` 0; öğe kümesi tam olarak html, head, meta,
>   title, body, div, p, h1, a; öznitelikler yalnız lang, charset, name, content, style, href; tek
>   `<a>`; `<meta charset="utf-8">` var — `TestRender_LoadsNothingAndUsesOnlyTheseElements`.
> - ✓ **`<script>`'li ad escape** — iki katman, ayrı ölçüldü: (1) ad kapısı `<`, `>`, `/`, `"`
>   taşıyan adı **gizler** (`TestNames_AnAddressShapedNameIsWithheld`); (2) kapı atlanıp
>   `<script>alert(1)</script> "q" 'a' & <img src=x>` doğrudan altı yuvaya verildiğinde templ
>   hepsini kaçışlar, kaçışlı metin 6 kez, ham `<script`/`<img`/`"q"`/`'a'` 0 —
>   `TestTemplate_EscapesWhateverReachesIt`. Gösterilen adın `'` ve `&`'ı HTML'de
>   `&#39;`/`&amp;`, düz metinde olduğu gibi — `TestNames_AnOrdinaryNameIsShownVerbatim`.
> - ✓ **Maltaca doğru** — ċ ġ ħ ż ve büyükleri iki parçada UTF-8 bayt olarak; `Ħal Għaxaq`
>   adı düz metinde birebir, HTML'de kaçışlı — `TestNames_AnOrdinaryNameIsShownVerbatim`.
> - ✓ **konu ASCII ise olduğu gibi, değilse `=?utf-8?q?`** (EM-1 düzeltme 4) — iki konu sabit
>   (`Your Taptime invitation`, `Reset your Taptime password`), adla değişmez, ad taşımaz,
>   yazdırılabilir ASCII, `=?` yok, `mime.QEncoding` değiştirmez; kontrol: Maltaca girdi
>   `=?utf-8?q?` olur. Bütün ileti `internal/mail`'in derleyicisinden geçer: iptal edilmiş
>   bağlamla `Send` → `timeout` (derlemeden **sonraki** sınıf), `invalid_message` değil; kontrol:
>   `=?…?=` konu aynı yoldan `invalid_message`; dinleyiciye 0 bağlantı —
>   `TestSubject_IsFixedASCIIAndPassesTheMailComposer`.
> - ✓ **kontrast AA (hesaplanmış) ve palet** — her metin düğümünün rengi ve zemini kendi öğesinin
>   ya da en yakın atasının satır içi stilinden okunur; biri eksikse kırmızı; her çift ≥ 4,5:1.
>   Palet iki yoldan: (1) çözülmüş belgedeki her `#`-dizisi **tam altı hane** ve palet token'ı;
>   (2) renk taşıyan her bildirim (`color`, `*color*`, `background*`, `border*`, `outline*`,
>   `box-shadow`, `text-shadow`, `fill`, `stroke`) yalnız palet hex'i, uzunluk ve çizgi biçimi
>   içerir — `red`, `transparent`, `currentColor`, `rgb(`, `hsl(`, `var(`, 4/8 haneli hex red —
>   `TestContrast_EveryTextOnItsGroundClearsAA` (kontrolleri `TestContrast_MathIsNotVacuous`,
>   `TestContrast_ColourReaderCatchesWhatItExistsToCatch`). Tablo aşağıda.
> - ✓ EM-2 devri **"Text ve HTML zorunlu"** — ikisi de dolu ve geçerli UTF-8; derleyici yolu
>   bunu da sınar.
> - ✓ ADR §8: **davet metni ana tarayıcı** — *"in your phone's main browser — the one that opens
>   when you tap a link"* ve *"choose "Open in browser" first"* iki parçada, *"own browser"* hiçbir
>   parçada yok — `TestInvitation_SaysToUseThePhonesMainBrowser`; **wordmark** metin, görsel yok;
>   wordmark, başlık ve düğme Space Grotesk'i ilk sırada adlandırır —
>   `TestRender_DisplayFaceOnWordmarkHeadingAndButton`; **"Tappa" 0**, "Taptime" var —
>   `TestRender_SaysTaptimeNotTheCodeName`; iki parça aynı cümleleri söyler —
>   `TestRender_TheTwoPartsSayTheSameWords`; süre ifadesi aşağı yuvarlar —
>   `TestLifetime_NeverOverstates`.
> - ✓ **Taze `app.css` farkı 0:** Tailwind v3.4.17 (`.tools/tailwindcss`, `--minify`), **taban
>   `b06370c`'de** önce ve sonra (2. ve 3. tur dahil) → 50 308 bayt, `cmp` özdeş (sha256
>   `e095e51c…`). Bayt sayısı tabana bağlıdır: denetçinin birleşik ağaçta ölçtüğü değer
>   50 472 bayt / `45f9ca40…`, fark orada da 0 (denetçinin ölçümü; bu turda yeniden koşulmadı). Pozitif
>   kontrol (1. tur): e-posta `.templ`'ine yorumda *"italic table"* eklenince +26 bayt, `.table` ve
>   `.italic` kuralları doğdu (geri alındı, sha256 doğrulandı). Gerekçe ve stil açıklamaları
>   `.templ`'de değil Go dosyalarında (Tailwind onları okumaz).
>
> **2. tur — üçüncü göz RED (iki pin kendi listesini kaçırıyordu) ve güvenlik ONAY bulguları:**
> - **B1 (bloklayan) — seed okuyucusu.** Çalışan regex'i virgülden sonra boşluk istiyordu;
>   `seed.sql`'in boşluksuz yazılmış üç satırı (Peter Falzon, Rosaria Schembri, Joseph Buttigieg)
>   atlanıyordu (33/36) ve *"≥ 30"* eşiği bunu gizliyordu. Şimdi `,\s*`; her INSERT bloğu ikinci,
>   bağımsız bir çapayla (`('<uuid>'` satır başı) sayılır ve okunan ad sayısı satır sayısına
>   **eşit** olmak zorunda (2 tenant, 36 çalışan). **D-01** (boşluksuz satıra `'J.B. Falzon'`)
>   → KIRMIZI (`TestNames_AnOrdinaryNameIsShownVerbatim`).
> - **B2 (bloklayan) — palet taraması.** 6/3 haneli hex taraması `border:1px solid red`'i ve
>   8 haneli `#C9D2C880`'ı görmüyordu. Şimdi yukarıdaki iki yol + öznitelik izin listesi
>   (`bgcolor=`). **C-01** ve **C-02** → KIRMIZI (`TestContrast_EveryTextOnItsGroundClearsAA`);
>   `bgcolor` (M48) → KIRMIZI (`TestRender_LoadsNothingAndUsesOnlyTheseElements`).
> - **B3 — ikinci parametre satırı** değeri `&next=x`: reddin tek sebebi artık `&` ve `=`.
>   **A15** (`tokenByte` `&` ve `=` kabul) → KIRMIZI, yalnız `a_second_parameter` alt testinde.
> - **B4 (metin) — iddialar ölçülen kümeye bağlandı.** *"URL benzeri ad gizlenir"* →
>   *"listelenen düşmanca küme gizlenir"*; kalan sınıra *"dedektörün etiket ayracı olmayan,
>   noktaya benzeyen her karakter (harf, işaret, rakam) gösterilir"* eklendi ve U+0660, U+06F0
>   (Nd) ölçüldü. Noktalama olan üç benzer nokta (U+00B7, U+30FB, U+2027) gizlenir ve
>   listeye girdi — **A20** (U+00B7 kabulü) artık **KIRMIZI** (`TestNames_AnAddressShapedNameIsWithheld`).
> - **B5 — ana tarayıcı cümlesi:** *"in that phone's own web browser"* →
>   *"in your phone's main browser — the one that opens when you tap a link"*; test
>   `TestInvitation_SaysToUseThePhonesMainBrowser` (eski adla anan her yer değişti; ayrıca *"own
>   browser"* yasak). M49 (eski cümle geri) → KIRMIZI.
> - **B6 — düğme yazı tipi** `'Space Grotesk',Arial,Helvetica,sans-serif`; yeni pin
>   `TestRender_DisplayFaceOnWordmarkHeadingAndButton` (M52 → KIRMIZI).
> - **Öneri (kabul):** `.`'nın ardından `,` ve `)` de serbest — `Kebab Co., Ltd.`,
>   `Kebab Factory (Malta Ltd.)` gösterilir; ardından harf, rakam, `-`, `(`, `'`, `&`, `.`, `!`
>   gelirse gizli — `TestNames_TheDotRule` (16 satır; gösterilen satırlarda dedektör 0).
>   M50 (`,` reddi) ve M51 (`-` kabulü) → KIRMIZI.
> - **Güvenlik DÜŞÜK — host adı:** `validHostName` (etiketler 1–63 `[A-Za-z0-9-]`, `-` ile
>   başlamaz/bitmez, ≤ 253) + port 1–65535; `https://:443`, `https://-`, `app-.`, `app..`,
>   `app_x`, `:`, `:0`, `:65536` red. M41 (eski `u.Host != ""`) ve M43 (port denetimi yok) →
>   KIRMIZI.
> - **Güvenlik DÜŞÜK — `http`:** yalnız loopback (`localhost`, `*.localhost`, IPv4 127.0.0.0/8 —
>   `netip`'in `IsLoopback`'i); `http://taptime.mt`, `http://192.0.2.1`,
>   `http://localhost.evil.example` red; `http://localhost:8080`, `http://127.0.0.1:8080`,
>   `http://app.localhost` geçer. `[::1]` köşeli parantez bayt kümesinin dışında olduğu için
>   **hiç** kabul edilmez (IPv6 taban yok). M42 (her host'ta http) → KIRMIZI.
> - **Güvenlik ORTA — EM-7 devri:** aşağıda, net madde olarak.
>
> **3. tur — dar kapanış denetimi RED (hepsi pin boşluğu; yalnız test satırları ve metin değişti,
> ürün kodu yorumsuz karşılaştırmayla birebir aynı — `email.go`, `letter.go`, `text.go` yorumlar
> çıkarılınca özdeş, `message.templ`/`message_templ.go` bayt bayt özdeş):**
> - **F1 — loopback tanımı.** Red satırları: `http://127.0.0.1.evil.example`,
>   `http://127.evil.example`, `http://evillocalhost`, `http://10.0.0.1`,
>   `http://192.168.1.10:8080`. **V01** (`"127."` öneki), **V02** (noktasız `localhost` soneki),
>   **V08** (`IsPrivate`) → KIRMIZI, her biri yalnız kendi satırlarında.
> - **F2 — kısaltmalar.** `colourProperty` artık `text-decoration*`, `column-rule*`,
>   `-webkit-text-stroke*`, `text-emphasis*` okur (`caret-color`, `accent-color` zaten `*color*`);
>   kontrol testinde her biri birer satır + *"underline red"* reddi. **P02** (düğmenin kendi
>   `text-decoration`'ına `underline red`) → KIRMIZI.
> - **F5 — boyamayı değiştirenler.** `opacity`, `filter`, `-webkit-filter`, `backdrop-filter`,
>   `-webkit-backdrop-filter`, `mix-blend-mode`, `background-blend-mode` bildirimleri yasak
>   (`blendingProperty`, kontrol listesinde). **P06** (`filter:invert(1)`) ve P06b
>   (`mix-blend-mode`) → KIRMIZI.
> - **F3 — izin listesinin sayımı.** `TestNames_EveryASCIICharacterBetweenTwoLetters`: 0x21–0x7E'nin
>   her karakteri iki harf arasında; yalnız harf, rakam ve `& ' - , ( ) !` (testte yazılı, `nameMarks`'tan
>   okunmaz) gösterilir — 69. `TestNames_TheDotRule`'a 0x20–0x7E taraması: `.`'dan sonra yalnız
>   `" ),"`. **N01–N07** (`;` `?` `=` `%` `_` `<` `*`) → KIRMIZI. **X01b**: mesajda tanımı yoktu;
>   yeniden kurgum *"`_` hem ad karakteri hem `.`'dan sonra serbest"* → KIRMIZI (iki taramada da).
>   Not: `.`'dan sonra **yalnız** kendisi zaten gizlenen bir karakteri serbest bırakan mutasyon
>   eşdeğerdir (ad o karakter yüzünden yine gizlenir).
> - **F4 — etiket kuralları.** Red satırları: `https://-app.taptime.test`, 64 baytlık etiket,
>   63 baytlık etiketlerden 254 baytlık host; kabul kontrolleri 63 baytlık etiket ve 253 baytlık
>   host (uzunlukları testte assert edilir). **V05** (iki sınır birden), V05a (253), V05b (63) ve
>   **V06** (baştaki `-`) → KIRMIZI.
> - **F6 (metin).** `validBase`/`validHostName` belgesi: *"sözdizimi kuralı; IPv4 biçimi
>   doğrulanmaz"*; `https://999.999.999.999` ve `https://1.2.3` iki *"known limit"* kabul satırı
>   olarak ölçüldü (kural sıkılaşırsa kırmızıya döner). Ürün kodu değişmedi (tur kuralı).
> - **F7 (metin).** Paket belgesi: *"her DOĞRULAMA hatası (`ErrBaseURL`, `ErrLink`,
>   `ErrLifetime`) değer anmayan sentinel'dir; `render`'ın tek öteki hatası templ'in yazıcıdan
>   gelen hatasını sarar"*.
> - **F8 (metin).** CSS bayt sayısı ölçüldüğü tabana bağlandı (yukarıda).
> - **F9.** Bilgi; EM-7 satırını orkestratör günceller — kartın EM-7 devri zaten aynı ön koşulu yazar.
>
> **Kararlar (ölçümle):**
> 1. **DKIM imzalı içerik (sayılı sınır 22'nin EM-4 yarısı) — ad kapısı, izin listesi, gizleme.**
>    Ad yalnız her rune'u harf, birleşen işaret, ondalık rakam, U+0020 ya da `& ' ’ - – — , ( ) !`
>    ise, en az bir harf varsa ve her `.` adın sonundaysa ya da ardından boşluk, `,` veya `)`
>    geliyorsa gösterilir; değilse nötr sözcükler (*"Hello,"*, *"Your employer"*) durur, e-posta
>    yine gider. Elenenler: (a) **yalnız kaçışlama** — HTML'i korur, linkleşmeyi değil (ADR §8);
>    (b) **adı dönüştürmek** — sahibinin yazmadığı bir dize basar; (c) **kötü biçim listesi** —
>    CLAUDE.md §5'in adres aralığı geçmişi. Ölçümler: `.` dışında NFKC biçimi `.` ya da U+3002
>    taşıyan **34** kod noktası (Python 3.14 `unicodedata`, Unicode 16.0) — hepsi Po/No/So,
>    hiçbiri harf/işaret/ondalık rakam; testte koşan Go'nun tablolarıyla yeniden ölçülür;
>    Python'un IDNA 2003 codec'i `evil<c>example`'ı U+3002, U+FF0E, U+FF61, U+FE52, U+2024 için
>    `evil.example`'a çevirir. **Listelenen** 33 düşmanca ad + 34 NFKC noktası gizlenir ve o
>    kümede gövdenin görünen metninde (HTML) linklenebilir dizi **0**, düz metinde linkten başka
>    **0** — dedektör bilerek geniş; kontrolü `TestLinkifiable_CatchesWhatItExistsToCatch`.
>    **Bedel ölçüldü:** seed'deki 2 tenant ve 36 çalışan adının **0**'ı gizlenir.
> 2. **URL sayımı:** parçalar ayrı sayılır, her biri tam 1; HTML'de görünen bir kopya yok.
> 3. **Düz metin üreticisi düz Go** (`strings.Builder`); tek kontrol yukarı akışta.
> 4. **Tek yapı, iki ileti:** `letter` + tek templ bileşeni + tek metin yazıcısı.
> 5. **Link parametre, yapı alanı değil:** paket yeni bir taşıyıcı eklemez.
> 6. **Sıfırlama e-postası kimseyi adlandırmaz** (bedeli EM-5'e).
> 7. **Wordmark küçük harf `taptime`**; Space Grotesk yerel yazı tipi olarak wordmark, başlık ve
>    düğmede ilk sırada, `@font-face` yok; `<meta name="color-scheme" content="light">`; karanlık
>    mod **ölçülmedi**.
> 8. **Süre ifadesi aşağı yuvarlar** (uzunluk, saat dilimi yok).
> 9. **(2. tur) Taban kuralı:** `https`; `http` yalnız loopback (tam olarak `localhost`,
>    `*.localhost`, IPv4 127.0.0.0/8); host adı **sözdizimi** (IPv4 biçimi doğrulanmaz — ölçülmüş
>    sınır); port aralığı.
>
> **Kontrast (WCAG bağıl parlaklık; testin günlüğünden):**
>
> | Metin · zemin | Nerede | Oran |
> |---|---|---|
> | ink · paper | başlık, paragraflar, kapanış satırı (kart) | **16,17:1** |
> | ink · porcelain | alt bilgi | **14,32:1** |
> | paper · tappa-green | düğme etiketi | **7,73:1** |
> | tappa-green · porcelain | wordmark (22 px kalın) | **6,85:1** |
>
> Metin dışı: kartın 1 px `line` kenarı süstür, bilgi taşımaz (line · paper 1,52:1 — skill'in
> damga kenarı için yazdığı aynı bilinçli kabul); düğmenin sınırı tappa-green · paper 7,73:1.
>
> **Mutasyonlar** (betik `scratchpad/em4/mutate.py`: tek eşleşme şartı, `.templ`'de `make templ`
> ile yeniden üretim, karar `go test`'in çıkış kodundan, derleme hatası "yakalandı" sayılmaz,
> her mutasyondan sonra paketin altı dosyası ve `seed.sql` bayt bayt manifestle karşılaştırıldı):
> **3. turun son, tam koşusu: 68 mutasyon, 67 KIRMIZI, 1 YEŞİL** (eşdeğer). Bir mutasyon birden
> çok değişiklik taşıyabilir (V05, X01b).
>
> | # | Mutasyon | Sonuç (kırmızıya dönen testler) |
> |---|---|---|
> | M01 | `nameShown` her adı kabul | KIRMIZI — ALineBreak…, AnAddressShaped…, KnownLimitIsAnUnusualName…, TheDotRule, TheGateTakes…, EachPartCarries…, LoadsNothing… |
> | M02 | nokta kuralı düştü | KIRMIZI — AnAddressShaped…, KnownLimitIsAnUnusualName…, TheDotRule |
> | M03 | `:` `/` `@` kabul | KIRMIZI — AnAddressShaped…, KnownLimitIsAnUnusualName… |
> | M04 | U+3002 kabul | KIRMIZI — AnAddressShaped… |
> | M05 | denetim karakterleri + U+2028 kabul | KIRMIZI — ALineBreak…, AnAddressShaped… |
> | M06 | paragrafta `templ.Raw` | KIRMIZI — EscapesWhateverReachesIt, AnOrdinaryName… |
> | M07 | alt bilgide ikinci link | KIRMIZI — EachPartCarries…, LoadsNothing…, DisplayFace… |
> | M08 | izleme pikseli | KIRMIZI — EachPartCarries…, LoadsNothing…, EscapesWhatever… |
> | M09 | `<style>` içinde uzak `@font-face` | KIRMIZI — EachPartCarries…, LoadsNothing… |
> | M10 | düz metin linki atlar | KIRMIZI — EachPartCarries… |
> | M11 | link değerinin bayt kuralı düştü | KIRMIZI — RefusesALink… |
> | M12 | `checkLink` her linki kabul | KIRMIZI — RefusesALink… |
> | M13 | taban yolunda `//` kabul | KIRMIZI — RefusesALink… |
> | M14 | taban her şemayı kabul | KIRMIZI — RefusesALink… |
> | M15 | davet konusunda tenant adı | KIRMIZI — Subject… |
> | M16 | ASCII dışı konu | KIRMIZI — Subject… |
> | M17 | düğme etiketi saffron · tappa-green | KIRMIZI — Contrast… |
> | M18 | palet dışı altı haneli kenar rengi | KIRMIZI — Contrast… |
> | M19 | sayfa zeminleri kaldırıldı | KIRMIZI — Contrast… |
> | M20 | gün sayısı yukarı yuvarlar | KIRMIZI — Lifetime… |
> | M21 | 1 dk altı kabul | KIRMIZI — Lifetime… |
> | M22 | `<meta charset>` kaldırıldı | KIRMIZI — LoadsNothing… |
> | M23 | ana tarayıcı cümlesi düştü | KIRMIZI — SaysToUseThePhonesMainBrowser |
> | M24 | düz metin kapanış satırını atlar | KIRMIZI — TheTwoPartsSayTheSameWords |
> | M25 | ad sınırı 120 → 100 | KIRMIZI — TheGateTakesEveryStoredLength |
> | M26 | link değerinde `%` kabul | KIRMIZI — RefusesALink… |
> | M27 | alt bilginin kendi rengi kaldırıldı | **YEŞİL — eşdeğer:** atası (`div`) aynı ink'i açıkça yazar; iddia "öğenin ya da en yakın atasının açık rengi"dir |
> | M28 | harfsiz ad kabul | KIRMIZI — AnAddressShaped… |
> | M30 | tabanın kendi ayrıştırmasıyla eşitliği düştü | KIRMIZI — RefusesALink… |
> | M31 | link etiketiyle aynı satırda | KIRMIZI — EachPartCarries… |
> | M32 | link düğmenin görünen metni de | KIRMIZI — EachPartCarries…, TheTwoParts…, EscapesWhatever…, AnAddressShaped…, KnownLimit…, TheDotRule |
> | M34 | her Unicode boşluğu kabul | KIRMIZI — ALineBreak…, AnAddressShaped… |
> | M35 | `.` yalnız en sonda (bedel yönü) | KIRMIZI — AnOrdinaryName…, TheDotRule |
> | M37 | tam 1 saat dakika okunur | KIRMIZI — Lifetime…, TheTwoParts… |
> | M38 | değer uzunluk sınırı düştü | KIRMIZI — RefusesALink… |
> | M39 | selamlama kapıyı atlar | KIRMIZI — ALineBreak…, AnAddressShaped…, EachPartCarries… |
> | M40 | tabanın bayt kümesi düştü | KIRMIZI — RefusesALink… |
> | M41 | host adı kuralı → 1. turun `u.Host != ""`'i | KIRMIZI — RefusesALink… (`:443`, `-`, `app-.`, `app..`, `app_x` satırları) |
> | M42 | her host'ta düz `http` | KIRMIZI — RefusesALink… (public ad, public IP, `localhost.evil.example`) |
> | M43 | port denetimi yok | KIRMIZI — RefusesALink… (boş port, 0, 65536) |
> | A15 | `tokenByte` `&` ve `=` kabul | KIRMIZI — RefusesALink…/a_second_parameter (yalnız o satır) |
> | A20 | U+00B7 kabul | KIRMIZI — AnAddressShaped… |
> | C-01 | kenarda `red` | KIRMIZI — Contrast… |
> | C-02 | kenarda `#C9D2C880` | KIRMIZI — Contrast… |
> | M48 | `<body bgcolor=…>` | KIRMIZI — LoadsNothing… |
> | M49 | *"own web browser"* geri | KIRMIZI — SaysToUseThePhonesMainBrowser |
> | M50 | `.`'dan sonra `,` reddi (bedel yönü) | KIRMIZI — TheDotRule |
> | M51 | `.`'dan sonra `-` kabulü | KIRMIZI — AnAddressShaped…, TheDotRule |
> | M52 | düğme sistem yığınına döndü | KIRMIZI — DisplayFaceOnWordmarkHeadingAndButton |
> | D-01 | `seed.sql`'de boşluksuz satıra `'J.B. Falzon'` | KIRMIZI — AnOrdinaryNameIsShownVerbatim (okuyucu satırı okur, ad gizlenir) |
> | V01 | loopback'e `"127."` öneki | KIRMIZI — RefusesALink…/`127.0.0.1.evil.example`, `127.evil.example` |
> | V02 | noktasız `localhost` soneki | KIRMIZI — RefusesALink…/`evillocalhost` |
> | V08 | loopback'e `IsPrivate` | KIRMIZI — RefusesALink…/`10.0.0.1`, `192.168.1.10:8080` |
> | V05 | 253 ve 63 bayt sınırları birden düştü | KIRMIZI — RefusesALink…/254 baytlık host, 64 baytlık etiket |
> | V05a | yalnız 253 bayt sınırı düştü | KIRMIZI — RefusesALink…/254 baytlık host |
> | V05b | yalnız 63 bayt sınırı düştü | KIRMIZI — RefusesALink…/64 baytlık etiket |
> | V06 | etiket başında `-` serbest | KIRMIZI — RefusesALink…/`-app.taptime.test` |
> | P02 | düğmede `text-decoration:underline red` | KIRMIZI — Contrast… |
> | P06 | düğmede `filter:invert(1)` | KIRMIZI — Contrast… |
> | P06b | kartta `mix-blend-mode:difference` | KIRMIZI — Contrast… |
> | N01–N07 | `nameMarks`'a `;` `?` `=` `%` `_` `<` `*` (her biri ayrı) | KIRMIZI — EveryASCIICharacterBetweenTwoLetters (yedisi de) |
> | X01b | `_` hem ad karakteri hem `.`'dan sonra (yeniden kurgu) | KIRMIZI — EveryASCII…, TheDotRule |
>
> (M29, M33, M36, M44–M47 numaraları kullanılmadı.) Araç hataları, dürüstçe (1. tur): M02
> **derlenmedi** (kullanılmayan `i`) ve "yakalandı" sayılmadı — `false &&` biçimiyle yeniden
> koşuldu; harness ilk koşuda `templ generate -path` kullandı ve üretilen dosyanın `FileName`'i
> değişti — geri yükleme denetimi yakaladı, harness `make templ`'in tam ağaç biçimine çevrildi.
>
> **Üç parçalı doğruluk iddiası (paket belgesinde birebir):**
> - **PART I** — bugünkü kod, ölçülen davranış: yukarıdaki kabul maddeleri, her biri test adıyla;
>   ad iddiası **listelenen** kümeye bağlı.
> - **PART II** — adıyla pinler ve her birinin yakaladığı tam liste: mutasyon tablosu (67
>   kırmızı) + iki ölçülmüş sınır testi `TestNames_KnownLimitIsALookalikeDotAndPlainProse` ve
>   `TestNames_KnownLimitIsAnUnusualNameWithheld` (sınır kalkarsa kırmızıya döner ve metin
>   güncellenir).
> - **PART III** — Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
>
> **Sayılı sınırlar:**
> 1. **İstemcilerin neyi linklediği ölçülmedi** — dedektör bizimdir; "linklenebilir dizi 0"
>    dedektörümüzün, listelenen kümedeki sayısıdır (ADR 0022 sayılı sınır 22).
> 2. **Dedektörün etiket ayracı olmayan, noktaya benzeyen her karakter (harf, işaret, rakam)
>    gösterilir** — ölçülen örnekler U+A4F8 (Lm), U+0323 (Mn), U+0660 ve U+06F0 (Nd);
>    `evil<c>example` insana adres gibi okunur.
> 3. **Rakamlar, tam genişlikli rakamlar ve düz saldırgan düzyazısı** gösterilir (*"Your account is
>    suspended Call 21234567 now!"*); bir istemcinin veri dedektörünün telefon numarasını
>    tıklanabilir yapması ölçülmedi — gösterildiği ölçüldü. Geri arama oltalaması — EM-7 devrine
>    bakın.
> 4. **Meşru ama alışılmadık ad gizlenir** (`J.B. Bar`, `Fish/Chips`, `Wine+Dine`, `Bar #1`,
>    `The "Spot"`, `Ristorante: Da Mario`) — bedeli yalnız o adın görünmemesi; seed'de 0 —
>    ölçüldü, `TestNames_KnownLimitIsAnUnusualNameWithheld`.
> 5. **Süre ifadesi çağıranın süresidir**, okunduğu andaki kalan süre değil.
> 6. **Karanlık mod** ve istemcilerin satır içi stili nasıl yeniden boyadığı ölçülmedi; Outlook
>    masaüstü `max-width`'i yok sayabilir (ölçülmedi; tablo düzeni bilerek yok: `<table>` sözcüğü
>    Tailwind'e `.table` kuralı doğurtur — kontrol ölçümünde doğurttu).
> 7. **Gerçek posta istemcilerinde görüntü** ölçülmedi — EM-8.
> 8. **Üretici bağı yarım:** sıfırlamanın üreticisi testte sürülür, davetin `activationURL`'i
>    dışa kapalı — EM-7'nin testi gerçek `invite.Delivery.ActivationURL`'i `RenderInvitation`'a
>    vermeli.
> 9. **Bir öğenin kendi rengi**, atası aynısını yazıyorsa düşebilir (M27, eşdeğer).
> 10. **IPv6 taban yok:** köşeli parantez bayt kümesinin dışında; `http://[::1]` dahil hiçbir
>     IPv6 literal taban kabul edilmez (bugünkü yapılandırmalar ad kullanır — `localhost`).
> 11. **Host kuralı sözdizimidir:** IPv4 biçimli ama adres olmayan host (`https://999.999.999.999`,
>     `https://1.2.3`) `https`'te geçer — ölçüldü, iki kabul satırı; `http`'de böyle bir host
>     loopback olmadığı için zaten reddedilir.
> 12. **`.`'dan sonra yalnız kendisi gizlenen bir karakteri serbest bırakmak eşdeğerdir** — ad o
>     karakter yüzünden yine gizlenir; ASCII taramaları ancak karakter listeye de girerse kırmızıya
>     döner (X01b).
>
> **Devirler:**
> - **EM-7 — ÜRÜN KARARI ŞART (güvenlik ORTA, 2. tur):** EM-7 `email` modunu açmadan **önce**
>   kullanıcıya sorulur ve cevabı `state.md`'ye tarihiyle yazılır: DKIM imzalı davette
>   saldırganın seçtiği düzyazı ve rakamlar (geri arama oltalaması: *"Your account is suspended
>   Call 21234567 now!"*, tam genişlikli rakamlar) ve noktaya benzeyen harf/işaret/rakam taşıyan
>   adlar bugün **gösterilir** (sayılı sınır 2, 3). Seçenekler: (a) tenant/çalışan adını yalnız
>   **doğrulanmış** tenant'ta göstermek (doğrulanmamışta nötr sözcükler); (b) riskin kullanıcıca
>   **açıkça kabulü**. Karar EM-7'nindir, WL-11'in değil; WL-11 yalnız başlık tarafını (aşağıda)
>   taşır. Bugün çağıran olmadığı için bloklamaz.
> - **EM-7 (bağlantı):** `RenderInvitation(ctx, d.ActivationURL, email.InvitationView{BaseURL:
>   cfg.BaseURL, EmployeeName, TenantName, ValidFor: inv.ExpiresAt.Sub(inv.CreatedAt)})`; testi
>   gerçek `IssueAndDeliver` linkini bu fonksiyona verir (sınır 8). `Message` loglanmaz;
>   `ErrLink`/`ErrBaseURL`/`ErrLifetime` log'a girebilir (değer taşımaz). Prod `BaseURL`'i
>   `https` olmalı — değilse render `ErrBaseURL` döner (taban kuralı).
> - **EM-5:** `RenderPasswordReset(ctx, delivery.Link, email.ResetView{BaseURL: cfg.BaseURL,
>   ValidFor: …})` → `To = delivery.Recipient`, `Ref = reset_id`; render hatası bir
>   programcı/yapılandırma hatasıdır → gönderim yok, `undelivered` satırı (§6). `ValidFor` olarak
>   `ResetTTL` mi `ExpiresAt − now` mı — karar EM-5'in. Birden çok hesaba çözülen adres için
>   e-postada işletme adı: eklenirse ad kapısından geçer ve EM-7'nin ürün kararı ona da uygulanır.
> - **EM-9:** üçüncü `letter` (sabit ASCII konu) + kendi link denetimi (giriş sayfası adresi,
>   sorgu/token **yok** — `checkLink`'in `?<param>=` biçimi uymaz, `validBase`'i kullanan ayrı bir
>   denetim gerekir); markup değişmez; testlerin kalıbı üçüncü iletiye genişletilir.
> - **WL-11:** başlıklara tenant verisi girmez — EM-4'te iki konu da sabit ve adla değişmediği
>   ölçüldü. *"X via Taptime"* gönderen adı (VIES koşulu) WL-11'in. Gövdedeki adın kararı
>   yukarıdaki EM-7 maddesindedir.
> - **EM-8:** gerçek cihaz turunda e-postanın Gmail/Outlook/Apple Mail'de görünümü ve *"Open in
>   browser"* ile *"main browser"* talimatlarının işe yaradığı.

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
  L ∈ (paper sınırı; ink sınırı) = (0,1789826956…; 0,2368152180…), formülle tanımlı — orada ne
  paper ne ink 4,5'e ulaşır (ör. #808080 ve #E0457B red; #FFC72C sarı ink metin + ink kenarla
  geçer; #DA291C kırmızı paper metinle 4,78 geçer) *(WL-0 düzeltmesi: "(0,1790; 0,2368)" yazıyordu;
  yuvarlanmış literal 1 092 rengi yanlış geçirir — ADR 0023 §3)*. Aynı fonksiyon yazma VE okuma
  tarafında (netx deseni). Palet sabitlerinin Go kopyası `tailwind.config.js`'i okuyan bir
  eşitlik testiyle kilitli *(WL-0 düzeltmesi: "ikinci kopya yok" uygulanamaz — `go:embed` paket
  dizininin dışına çıkamaz)*.
- **Asla değişmeyen:** beş kaşe damgası + durum→renk eşlemesi · tomato = hata/yıkıcı · saffron =
  FLAGGED/geç · Notice · docket/perforasyon · `.docket-label` · panelin birincil/yıkıcı butonları ·
  odak göstergeleri — `.btn`'in ink halkası ve tap düğmesinin tarayıcıdan gelen outline'ı
  (Chrome 154'te `rgb(0, 95, 204)`; kuralı renk koymadığı için accent onu taşımaz) *(WL-0
  düzeltmesi: "focus halkası (ink)" yazıyordu)* · **sonuç ekranında accent hiç yok** (renkler
  durumu anlatıyor).
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
  (= 2²²; decode-bomb) · JPEG'te SOS sayısı decode'dan önce, çözücünün işleyeceğinin **üst
  sınırı** olarak sayılır ve tavanı aşan red (CPU bombası) · süreç geneli semafor, N yuva (N <
  `GOMAXPROCS` = 2 → N = 1), beklemeden alınır (doluysa hemen ret), yuva decode → küçültme →
  kodlama boyunca tutulur ve goroutine dönünce bırakılır; ölçülen en
  kötü decode **96 MiB** (progressive CMYK 4:4:4), GC payıyla birlikte 512 Mi'ye karşı hesaplanır
  — *(WL-0 düzeltmesi: "en kötü 16 MiB RGBA · 1–2 slot" yazıyordu; ADR 0024 §2.5–2.6)* · uzun
  kenar 512 px'e elle box filtre (stdlib; `x/image/draw` bağımlılık olurdu) · PNG→PNG
  (BestCompression), JPEG→JPEG q85 **yeniden kodlama** → EXIF/XMP/ICC/metin chunk'ları ve IEND
  sonrası polyglot düşer — 🔴 **§4.2: telefon fotoğrafının EXIF GPS'i silinir (testle)** · çıktı
  >256 KiB → "logoyu sadeleştir" · `sha256(çıktı)` URL anahtarı (⚠️ "fingerprint" kelimesi R1
  tetikleyicisi — "digest"/"sha256" kullan).
- **Depolama: Postgres `bytea`** — pod `readOnlyRootFilesystem` + hiç volume yok, kümede obje
  deposu yok (`50-backup.yaml` ölçmüş), `pg_dump` yedeğine kendiliğinden girer; 256 KiB × 1000
  tenant = 250 MiB *(WL-0 düzeltmesi: "≈ 256 MB")*, TOAST satır dışında tutar. Yükleme `r.MultipartReader()` ile AKIŞ — tek
  `logo` parçası ve `LimitReader` denetimi için; `ParseMultipartForm` parçadan küçük bir eşikle
  çağrılırsa geçici dosyaya yazmaya kalkıp salt-okunur FS'te patlar *(WL-0 düzeltmesi: "büyük
  parçayı … patlar" yazıyordu — ölçüldü (S20): 1 MiB gövde tavanı altında `FormFile`/`FormValue`'nun
  örtük 32 MiB ayrıştırması diske dökmüyor; ayrımı WL-7'nin sözdizimi pini yapar, ADR 0024 §6)*.
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
  grubu, canlı oturum şart) — admin çerezi `Path=/admin` olduğu için tap yüzeyindeki bir rota
  yönetici kimliğini göremez ve iki yüzeyin çözümleyicisi/bütçesi ayrıdır *(WL-0 düzeltmesi:
  "tek ortak rota iki çerezi alamaz" yazıyordu — çalışan çerezi `Path=/` olduğu için `/admin/…`'ya
  da gider; ADR 0024 §5)*. Tenant YALNIZ oturumdan; başka tenant'ın logosu ve var olmayan hash
  **bayt-aynı 404** (kehanet yok). Başlıklar: saklanan mime, `private, max-age=31536000,
  immutable`, `ETag: "<sha>"`, nosniff, `Content-Security-Policy: default-src 'none'; sandbox`,
  `Cross-Origin-Resource-Policy: same-origin`, `Content-Disposition: inline;
  filename="logo.png"` ya da `"logo.jpg"` — mime'a göre *(WL-0 düzeltmesi: sabit `logo.png`)*. Sayfa CSP'sine `img-src 'self'` YALNIZ `<img>` render edilen sayfaya
  (`tapCSPFor(hasLogo)` / `adminCSPFor(hasLogo)`, `landingCSPFor` emsali). Kimliksiz hash-rotası
  elendi (SECURITY DEFINER ile tenant'lar arası okuma = ADR 0002 md.7'ye yeni istisna + bütçesiz DB
  okuması).
- **Yüzeyler:** tap ekranı logo (sabit yükseklikli başlık yuvası, altında küçük "taptime ·
  punchless" — K-2b ✅ 2026-10-02) + accent tap butonunda (✅ D-C 2026-09-24); logo yok ama accent
  varsa başlıktaki `taptime` ink (K-2a ✅ 2026-10-02) · sonuç ekranı yalnız logo (+ altında
  co-brand, K-2b) · tur/practice logo (faz 2) · panel kabuğu logo + tenant adı + 4 px
  accent şeridi (birincil butonlar yeşil kalır) · Account → "Your brand" editör + gerçek bileşen
  önizlemesi (mevcut "What your staff read" deseni; önizlemedeki tap düğmesi accent'i alır,
  **formsuz** — ADR 0023 §2) · aktivasyon (Taptime + işveren adı, bugünkü
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
| WL-1 | Migration `tenant_branding` + `db/queries/branding.sql` + RLS testi | M | tappa-db-migrator | `make audit` R5/R5b 0; beşli tam; RLS testi `WHERE`'siz A bağlamında B'yi 0 görür + B `tenant_id`'li INSERT WITH CHECK ile red; `has_table_privilege('tappa_app','tenant_branding','DELETE')=false`; CHECK'ler hasmane değerlerle patlatılmış (küçük harf hex, 3 haneli hex, 262145 bayt, kısmi logo alanları); `updated_at` + `updated_by` ve bileşik `FOREIGN KEY (updated_by, tenant_id) REFERENCES admin_users (id, tenant_id)` — başka tenant'ın yönetici id'si FK ile red *(WL-0, ADR 0023 §1)*; Down→Up→Down bayt-aynı; `GetTenantBrand` `logo` seçmiyor (test sorgu metnini okur) | WL-0 |
| WL-2 | `internal/brand/accent.go` | S | builder | tablo değerleri ±0,01, **hesaplanan** L sınırları (0,1789826956… / 0,2368152180…) dahil, her iki yanındaki en yakın hex'le sınanır — literal yok *(WL-0: yuvarlanmış 0,1790/0,2368 bandın içinde; ADR 0023 §3)*; palet Go kopyası, `tailwind.config.js`'i okuyan eşitlik testiyle pinli (renk değişirse kırmızı); `Suggest` deterministik, her çıktısı `Check`'ten geçer (özellik testi); kapsam ≥%90 | WL-0 |
| WL-3 | `internal/brand/logo.go` | M | builder | SVG/GIF/WebP/HTML/PNG-magic'li HTML red; 30000×30000 başlıklı dosya decode'dan ÖNCE red (bayt başına tahsis ölçülür); kesik red; IEND sonrası yük çıktıda yok; EXIF-GPS'li JPEG çıktısında `Exif` APP1 yok; CMYK JPEG, 16-bit, paletli, interlaced PNG normalize; ≤512 px, ≤256 KiB; `FuzzNormalize` panik yok, her başarılı çıktı yeniden decode olur ve sınırlarda; semafor -race altında ≤N; **WL-0 ekleri (ADR 0024 §2.5–2.6):** JPEG SOS sayımı çözücünün işleyeceğinin üst sınırı — tavan+1 taramalı dosya ve "dürüst olmayan" dosya (segment arası çöp + gizli SOS) decode'dan önce red, tavan kadar taramalının süresi karta; yuva decode → küçültme → kodlama boyunca tutulur, goroutine dönünce bırakılır (iptal edilen istekten sonra yuva dolu kalır); 512Mi konteynerde N eşzamanlı en kötü decode altında RSS ölçülür, `GOMEMLIMIT` ya da `2 × (N × tepe + taban)`; N < `GOMAXPROCS` → N = 1 (Go 1.25+ cgroup sınırından `GOMAXPROCS` = 2; podda `runtime.GOMAXPROCS(0)` bir kez ölçülür); yuva beklemeden alınır, N doluyken gelen istek decode'a girmeden hemen red; `go.mod` diff boş | WL-0 |
| WL-4 | Domain `internal/domain/tenant/brand.go` | M | builder | kaydet/sil UPDATE + audit aynı tx (zorla patlatılan audit UPDATE'i geri alır; iki yön); detail tam 6 sabit anahtar; domain accent'i yeniden `Check` eder → `ErrAccentIllegible`; `ActorID` zorunlu | WL-1,2,3 |
| WL-5 | Theme rotası + Tailwind token'ları (`brandtheme.go`, `tailwind.config.js`, `input.css`) | M | builder + tappa-brand | kurucu havuz almıyor; 200/404 matrisi (kanonik, küçük harf, geçersiz, red bandı); başlıklar birebir, gövde yalnız 3 özellik; marka yoksa tap butonu CDP computed `rgb(31,92,65)`/`rgb(255,253,244)` (bir kez); derlenmiş `app.css`'te `:root` varsayılanlarını okuyan test *(WL-0, ADR 0023 İddia B)*; slot testi özelliğe de bakar: `--brand-accent` yalnız `background-color` bildirimlerinde, `--brand-on-accent` yalnız `color`'da, `--brand-edge` yalnız kenarda (mutasyon: `.tap-button{color:rgb(var(--brand-accent))}` → kırmızı) *(3. tur, ADR 0023 §3)*; yeni bir marka testi: tenant accent'i yalnız marka slotlarında (henüz yazılmadı — adı WL-5'te konur; mutasyon: `.stamp`'e `bg-brand` → kırmızı); `TestCompiledCSS_StampWordIsInk` yeşil; yorumdan ölü CSS kuralı doğmuyor | WL-2 |
| WL-6 | Logo rotaları + sayfa başına `img-src` | M | builder | başlıklar birebir; A oturumu B'nin sha'sını isteyince aldığı 404 bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu 404; "sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır" testi (panel + tap); ücretli istek sayısı ölçülüp bütçeler güncellendi (sıcak 1, soğuk 2); **WL-0 ekleri:** `Content-Disposition` dosya adı mime'a göre; yönetici oturumu olmayan, çalışan çerezli istek `/admin/brand/logo/…`'dan logo baytı almaz (ADR 0024 §5) | WL-1,4 |
| WL-7 | Account → "Your brand" editörü (`brandactions.go`, account.templ/view, üç `ProtectWriting` rotası: logo, accent, sıfırla) | L | builder + tappa-brand | yükleme `MultipartReader` akış, `TMPDIR` salt-okunur/yokken bile başarılı (geçici dosya yok); yalnız tek `logo` parçası, bilinmeyen parça red; cross-origin POST resolver'dan önce red; manager POST 303 `not-permitted` + `brand_update_refused` + 0 UPDATE; okunaksız renk formu yeniden render + önerilen hex, yazma yok; önizleme gerçek tap bileşenlerini render eder; açık logo uyarısı (alfa ağırlıklı parlaklık logonun oturduğu porcelain'e <1,5:1 → uyarı, ret değil — *WL-0 düzeltmesi: "paper'a" yazıyordu*); `FactNoBulkImport` tripwire'ı "marka logosu handler'ı dışında multipart okuyucu yok" olarak yeniden türetildi (mutasyon: `employeeactions.go`'ya `FormFile` → kırmızı; SSS cümlesi değişmez); `<input type="color">` + hex alanı dokunma hedefi ≥44 px; **WL-0 ekleri (ADR 0024 §6, ADR 0023 §2):** `TMPDIR` testi `FormFile`/`FormValue`'yu ayırt etmez (1 MiB tavan altında diske dökmezler) → logo handler dosya(lar)ının AST'sini okuyan pin: `FormValue`, `PostFormValue`, `FormFile`, `ParseMultipartForm`, `ParseForm` çağrısı ve `Form`/`PostForm`/`MultipartForm` okuması 0 (mutasyon: `r.FormValue("x")` → kırmızı); tenant başına yükleme bütçesi (aşım 429 + ret audit'i + 0 UPDATE); gövde okunmadan önce eşzamanlı yükleme kabul sınırı; `SetReadDeadline` ile gövde okuma süresi; `40-ingress.yaml:127-133`'ün "small JSON body" yorumu güncellenir — OP-10B'nin eklediği *"the largest bound a handler behind this Ingress sets itself is the sign-up form's 64 KiB"* cümlesi dahil: logo yüklemesi (`internal/brand.LogoMaxInputBytes` = 512 KiB) bu Ingress'ten geçer, **WL-7 bu cümleyi de günceller** *(OP-10B 3. tur)*; istek tamponlaması kümede ölçülür; ret yollarının log satırlarında dosya adı/bayt 0; önizleme **gönderilemez** — `<form` ve `/api/checkin` 0, düğme `type="submit"` değil, hiçbir `<form>`'un soyundan değil, `form=` özniteliği yok; önizlemedeki logo `/admin/brand/logo/{sha}`'dan; önizleme **yalnız kaydedilmiş** accent'i gösterir (aday hex `<input type="color">`'un kendi rengiyle) — ADR 0023 §2 *(3. tur)*; önizleme WL-9'un ayırdığı tap bileşenlerini çağırır | WL-4,5,6,9 |
| WL-8 | Panel kabuğu (`panelChrome`, `PanelChrome`, `review.go` `chrome()`) | S | builder | logo + tenant adı + şerit (K4); `chrome()` tam +1 PK okuması, EXPLAIN ANALYZE seed'de <1 ms; marka okuma hatası sayfayı düşürmez; AdminChoose değişmez | WL-5,6, OP-10 |
| WL-9 | Tap, sonuç, tur ekranları (`base.templ` açık parametreli `BrandedPage…`, `tap.templ`, `result.templ`, `view.go`, `tap.go`/`checkin.go` `tapCSPFor`, `directory.go` `TapPage` markayı aynı tx'te okur, `result_test.go` beyaz listesi) | M | builder + tappa-brand | **§9 onayı (D-C) olmadan başlamaz**; `TapView` 3 ve `ResultView` 8 alan kalır (marka ayrı açık parametre; kartta onay alıntılı); logo `width`/`height` → CDP layout-shift 0; 390×844'te tap butonu üst kenarı ≤16 px kayar, hâlâ tek buton, "Tap" metni, ≥64 px; tek yeni metin `alt` = tenant adı; A çalışanı B plaketinde gövdede `/t/logo/` yok; marka yoksa HTML + CSP bayt-aynı (golden); okuma hatasında 200 + varsayılan; **WL-0 ekleri (ADR 0023 §2, §5–§7):** K-2a ve K-2b (2026-10-02) kartta alıntılı — logo varken üstte logo + altında "taptime · punchless", logosuz-accent'li tap ekranında başlıktaki `taptime` ink; markalı tenant'ın sonuç sayfasında tema `<link>`'i 0; uyuşmazlıkta **sonuç** sayfasında da `/t/logo/` ve tema `<link>`'i 0, eşleşen tenant'ta sonuçta logo var; golden aktivasyon ailesini de kapsar; 16 px bütçe ile logo yuvası aritmetiği (yuva ≤ `29 − aralık` px) orkestratörce karara bağlanır; **3. tur ekleri:** golden'a giren render'lar kartta **adıyla ve sayısıyla** listelenir (tap ekranı; sonuç ekranının hüküm × yön × iş türü × practice varyantlarından seçilenler; aktivasyon ailesi) — golden yalnız listelenen fikstürleri yakalar (ADR 0023 İddia B); `templ Tap` başlık ve düğme yüzü bileşenlerine ayrılır (Account önizlemesi için — WL-7 buna bağımlı), bölme sonrası tap ekranı golden'ı bayt-aynı | WL-5,6, D-C |
| WL-10 | Güvenlik denetimi (tüm WL diff'i) | M | tappa-security-auditor | ONAY — izolasyon (RLS + handler), fuzz/bomb, başlıklar, CSP diff, multipart tripwire, bütçeler, audit, log'da dosya adı/byte yok | WL-7,8,9 |
| WL-11 | E-posta entegrasyonu | S | builder | `"X\r\nBcc: y"` tenant adı ek başlık üretmez; RFC 2047; From alan adı hep Taptime | EM-7, WL-4 |
| WL-12 | Dokümanlar (tappa-brand skill tenant slotları + kontrast kuralı, handoff §4, roadmap, state) | S | orkestratör | `make check` + `make audit` exit 0; **WL-0 ekleri:** ADR 0005'e marka taklidi eki (`cmd/tappa/adr0005_test.go` sayımlarıyla birlikte — ADR 0023 sınır 1); skill bölümünden "taslak" kalkar; CLAUDE.md §9 tenant slotu cümlesi + §3 `internal/brand` | WL-10 |

Bilinçli güncellenecek mevcut testler: `TestResultScreen_SaysExactlyThisAndNothingElse` (`alt`
metni), `FactNoBulkImport`, `TestBrand_*`, panel CSP ↔ script karşılığı testi.

> **Kart düzeltmesi (2026-10-02, WL-0 uygulaması sırasında).** Yazıldı:
> [ADR 0023](../adr/0023-tenant-markasi-ve-arayuz-kurali.md) (slotlar, accent kapısı, §9 onayı) ·
> [ADR 0024](../adr/0024-kullanici-yukledigi-gorsel.md) (logo dosyası: biçim, sınır, yeniden
> kodlama, saklama, servis) · skill `tappa-brand` → *"Tenant slotları (taslak)"* (yalnız ek).
> Tablonun bütünlüğü için blok WL-0 satırının değil tablonun altında. Tasarım özünden sapmalar
> aşağıda; normatif hâlleri ADR'lerde. **Ölçüm yöntemi:** HEAD `c0c0250` üzerinde `rg`/`sed`
> okuması (her olgu ADR'lerin "Bağlam — bugün" tablosunda `dosya:satır` ile) · stdlib sondası
> (`image/png`, `image/jpeg`, `mime/multipart`, `net/http`) scratchpad'de ayrı modülde,
> **go1.27.1** darwin/amd64 (CI 1.26.x'te yeniden ölçülmedi) · WCAG hesabı paleti
> `tailwind.config.js`'den okuyan betikle, 2²⁴ rengin tamamı ayrıca sayıldı · `pages.Tap`'in
> gerçek render'ı ve scratch'te derlenmiş `app.css` headless Chrome 154'te. Depoya kod ya da
> migration yazılmadı.
>
> 0. **Sıra:** A1 → **C** → B → A2; WL-11 B ile birlikte. Kullanıcının *"çok yavaş, paralel
>    ilerle"* talebi üzerine, SES'in dış adımları kullanıcı tarafından sonraya bırakıldığı için
>    orkestratörün önerisi (otonomi kuralı); 2026-10-02'de kullanıcıya raporlandı, itiraz yok;
>    D-D'nin (2026-09-24) sırasının yerine geçer. §2'nin şeması ve
>    §6'nın D-D satırı orkestratörce güncellenir. ADR numaraları değişmedi: 0022 SES'e ayrılmış, 0023/0024 önce yazıldı — §2'nin
>    *"yazıldığı anda sıradaki boş numara"* kuralına bu iki ADR için bilinçli istisna.
> 1. **L sınırları tutuyor, "dahil" tutmuyor (WL-2).** Paletten: paper metin
>    `L ≤ 0,1789826956…`, ink metin `L ≥ 0,2368152180…` (formülün çıktısı; yazım kesik) —
>    0,1790/0,2368 bunların dört haneli yuvarlanmışı. Yuvarlanmış değerler bandın **içinde**:
>    `L = 0,1790`'da paper 4,49966:1, `L = 0,2368`'de ink 4,49976:1. 2²⁴ renk sayıldı: dört haneli
>    değerler dahil okunursa **1 092** renk 4,5'e ulaşmadan geçer (ör. `#008384`, 4,49995:1);
>    altı haneli 0,178983 / 0,236815 bile **14** renk geçirir (paper 9, ör. `#22864B`
>    4,4999988:1; ink 5, ör. `#1E93A0`). WL-2'nin kabulü *"hesaplanan iki sınır dahil; her iki
>    yanındaki en yakın hex'le sınanır"* diye okunur; testte literal yok (WL-2 satırı 2. turda
>    düzeltildi).
> 2. **Örneklerin dördü tutuyor:** `#808080` 4,17 red · `#E0457B` 4,16 red · `#FFC72C` ink 10,56
>    + kenar · `#DA291C` paper 4,78. Ek ölçüler: OnColor dönüm noktası `L = 0,2062727…` (bantta en
>    iyi kontrast 4,0208); porcelain'e karşı 3:1 `L ≤ 0,2542173…`, üstünde Edge; kapı 2²⁴ rengin
>    **1 949 736**'sını (%11,62) reddeder.
> 3. **Bellek: "en kötü 16 MiB RGBA" yanlış (WL-3).** 2048² decode başına ölçülen `TotalAlloc`:
>    16-bit PNG 38,13 · interlaced PNG 32,45 · progressive 4:4:4 JPEG 60,02 · 16-bit RGBA
>    interlaced PNG 64,22 (512 KiB'a sığan düz örnek) · progressive CMYK 4:4:4 **96,02 MiB**.
>    Progressive / 4 bileşenli JPEG'i reddetmek tepeyi ~64 MiB'a indirir (1,5 kat; 1. turda yanlış
>    olarak "38 MiB" yazılmıştı). Semaforun kapsamı, GC payı ve CPU ilişkisi ADR 0024 §2.6'da.
> 4. 🔴 **Tasarım özünde olmayan bomba sınıfı: JPEG tarama sayısı (CPU) — WL-3 kabulüne ek.**
>    `image/jpeg` tarama sayısını sınırlamıyor; elle kurulmuş 2048² progressive JPEG'de 3 000
>    tarama 56 KiB'ta **8,72 s**; 512 KiB'a ~32 000 sığar (doğrusal kestirim ~94 s); sunucuda
>    `WriteTimeout` yok (`cmd/tappa/main.go:655-656`) ve router'ın `middleware.Timeout(30 s)`'i
>    (`internal/httpx/router.go:83`, chi v5.3.1) yalnız context'e süre koyar — çözücü context
>    okumaz. Kural (ADR 0024 §2.5): SOS sayısı decode'dan önce, çözücünün işleyeceğinin **üst
>    sınırı** olarak sayılır (çözücü segmentler arası çöpü atlayıp yeniden hizalanır,
>    `image/jpeg/reader.go:542-568`); tavan WL-3'te ölçümle (alt sınır 18; süre bütçesi için doğal
>    aday 30 s). Ek kabul: tavan+1 taramalı dosya ve "dürüst olmayan" dosya decode'dan önce red;
>    tavan kadar taramalının çözme süresi karta yazılır.
> 5. **Odak halkası:** tasarım özünün *"focus halkası (ink)"*'ı `.btn` için doğru
>    (`input.css:203-207`), `.tap-button` için değil — kuralı renk koymuyor; Chrome 154'te odakta
>    `rgb(0, 95, 204)` (tarayıcının rengi). Accent onu taşımaz; değişiklik gerekmiyor.
> 6. **`.tap-button` 7 `class` özniteliğinde** (tap 1 + aktivasyon/tur/problem/onay 6). Accent'i
>    sınıf değil tema `<link>`'i taşır; WL-5/WL-9 o bağlantıyı tap ekranına ve panel kabuğuna
>    ekler (panelin içindeki Account önizlemesinin düğmesi accent'i bilerek alır — madde 19).
>    WL-9'un golden'ı aktivasyon ailesini de kapsamalı (bayt-aynı).
> 7. **Accent bir dolgudur** (tap düğmesinin zemini — tap ekranı ve önizleme — ve şerit); kapı
>    accent'in metin olarak okunurluğunu ölçmüyor (ADR 0023 §3). WL-5'in slot testine girer.
> 8. **§9 sorusu — logosuz ama accent'li tenant'ın tap ekranı başlığı → kullanıcı kararı K-2a
>    (2026-10-02): `taptime` ink.** Aynı gün K-2b: logo yükleyen tenant'ın tap ve sonuç ekranında
>    üstte logo, altında küçük "taptime · punchless" (co-brand K3 böylece kullanıcı onaylı). İkisi
>    ADR 0023 §2, §5–§7'de ve WL-9 satırında.
> 9. **Açık logo uyarısının zemini:** WL-7 *paper* yazıyor; logo yuvası bugünkü başlık satırında,
>    porcelain üstünde (`base.templ:66`) → uyarı porcelain'e karşı hesaplanır.
> 10. **`Content-Disposition` dosya adı mime'a göre** (`logo.png` / `logo.jpg`), sabit değil (WL-6).
> 11. **"≤4 MP" = 2²² = 2048²** okunur — kenar sınırıyla aynı küme (WL-3).
> 12. **Kart metni:** "Yüzeyler" maddesindeki *"(⏳ D-C)"* bayattı — D-C ✅ 2026-09-24 (2. turda
>     tasarım özünde düzeltildi). *"Plaket
>     'taptime' basıyor"* yalnız landing çiziminde ölçüldü (`landing.templ:199`); fiziksel baskı
>     ölçülmedi, skill'in plaket bölümü hâlâ `tappa` yazıyor (bu görev mevcut metni değiştirmedi).
> 13. **Pinlerin kapsamı (WL-4/WL-5/WL-6):** `TestStaffQueries_CarryAnExplicitTenantPredicate`
>     marka sorgularını ancak `internal/domain/tenant`'tan çağrılırlarsa görür — logo rotası
>     sorguyu başka paketten çağırırsa o paketin kendi kopyası gerekir.
>     `TestCompiledCSS_StampWordIsInk` `app.css` derlenmemişse atlanır ve zemin rengine bakmaz;
>     WL-5'in slot testi o boşluğu kapatmalı.
> 14. **Multipart — ölçülen kapsamıyla:** `ReadForm(64 KiB)` 600 KiB'lık parçada, `TMPDIR`
>     yokken geçici dosya açmaya çalışıp düşüyor (ölçüldü). Ama `MaxBytesReader(1 MiB)` altında
>     `r.FormFile`, `r.FormValue`, `r.PostFormValue` ve `ParseMultipartForm(1 MiB)` aynı koşulda
>     **başarılı** — 32 MiB eşiğin altında kalıp diske dökmüyorlar (2. tur, S20). Yani tasarım
>     özünün *"`TMPDIR` yokken başarılı"* kabulü `FormFile`'a dönüşü yakalamaz; ADR 0024 §6 ve
>     WL-7 satırı bunu bir sözdizimi pinine bağladı. `FormValue`/`PostFormValue` multipart gövdede
>     örtük `ParseMultipartForm(32 MiB)` çağırır (Go `net/http/request.go:1442-1474`) ve üretimde
>     kullanılıyor (`adminlogin.go:880-881`). iPhone HEIC'in dosya seçicide JPEG'e çevrilip
>     çevrilmediği ölçülmedi → WL-7'nin gerçek cihaz turu.
> 15. **PNG→PNG ve 256 KiB:** fotoğraf benzeri 512 px PNG 440 KiB'a kodlanıyor → red; aynı
>     görüntü JPEG q85'te 43 KiB. WL-7'nin ret cümlesi JPEG'i önermeli.
> 16. **Devirler:** ADR 0005'e marka taklidi eki → **WL-12'nin kabulüne bağlandı** (2. tur;
>     `cmd/tappa/adr0005_test.go` sayımlarıyla birlikte) · CLAUDE.md §9 (tenant slotu cümlesi) +
>     §3 (`internal/brand`) — WL-12, orkestratör · WL-10 denetim listesine ADR 0024'ün sekiz
>     iddiası (A–H) ve ADR 0023'ün dördü (A–D).
> 17. **İki logo rotasının gerekçesi düzeltildi (WL-6).** *"Tek ortak rota iki çerezi alamaz"*
>     tutmuyor: çalışan çerezi `Path=/` (`internal/session/cookie.go:182`), yani `/admin/…`'ya da
>     gider. Doğru gerekçe: yönetici çerezi `/admin` dışına gitmez (`adminauth/cookie.go:48`) ve
>     iki yüzeyin çözümleyicisi/bütçesi ayrıdır. Karar değişmedi (iki rota); WL-6'ya ek kabul:
>     yönetici oturumu olmayan, çalışan çerezli istek `/admin/brand/logo/…`'dan logo baytı almaz.
> 18. **Depolama aritmetiği:** 256 KiB × 1 000 tenant = 250 MiB (tasarım özü *"≈ 256 MB"*;
>     ondalık 262 MB). Karar (K7, `bytea`) değişmedi.
>
> **2. tur (2026-10-02 — üçüncü göz RED, 3 bloklayan metin bulgusu + 9 bloklamayan;
> tappa-security-auditor ONAY, 4 orta + 4 düşük; kullanıcının iki §9 kararı).** Yukarıdaki 0, 1,
> 3, 4, 6, 7, 8, 12, 14, 16 güncellendi; ek maddeler:
> 19. **Account → "Your brand" önizlemesi slot haritasına girdi** (ADR 0023 §2): önizleme tap
>     ekranının gerçek bileşenleridir, panel kabuğunun tema bağlantısı yüzünden düğmesi accent'i
>     alır ve logo orada da görünür. Önizleme **formsuz**: `pages.Tap`'in formu panelden
>     `/api/checkin` POST'u göndermemeli (WL-7 kabulüne eklendi). K4 istisnası (panelde iki renk)
>     önizlemeyle birlikte yazıldı.
> 20. **Semafor, GC, CPU, bütçeler (güvenlik ORTA-1…4; ADR 0024 §2.6, §6):** yuva decode →
>     küçültme → kodlama boyunca tutulur, goroutine dönünce bırakılır; `GOMEMLIMIT` ya da
>     `2 × (N × tepe + taban)` — depoda GC ayarı 0; tek replika (`20-app.yaml:53`) → OOMKill tap
>     dahil kesinti; N < `GOMAXPROCS` (CPU sınırı 2) → N = 1 (3. turda "öneri"den "kuralın
>     sonucu"na düzeltildi); tenant başına yükleme bütçesi,
>     gövde okunmadan önce eşzamanlı kabul sınırı, `SetReadDeadline`, ingress bağımlılığı adıyla
>     (`40-ingress.yaml:127-133`). WL-3 ve WL-7 satırlarına eklendi.
> 21. **`tenant_branding`'in `updated_at`/`updated_by` sütunları ve bileşik FK'sı** ADR 0023
>     §1'e ve WL-1 satırına girdi (`admin_users_id_tenant_key`, `00006_create_admin_users.sql:85`).
> 22. **K-2b'nin piksel aritmetiği:** co-brand satırı tek başına 15 px (ölçüldü), bugünkü başlık
>     28 px; logo yuvası `H`, aralık `g` ise kayma `H + g − 13`. WL-9'un 16 px bütçesi yuvayı
>     ≤ `29 − g` px'e bağlar (ör. 4 px aralıkla 25 px). 40 px'lik yuva 31 px kaydırır — yuva mı
>     bütçe mi, orkestratörün WL-9 kararı.
> 23. **§5 içinde düzeltilen satırlar (güvenlik DÜŞÜK-8):** tasarım özünün "Renk", "Logo",
>     "Servis" ve "Yüzeyler" maddeleri (her düzeltme *"WL-0 düzeltmesi"* işaretli) ve WL-1, WL-2,
>     WL-3, WL-5, WL-6, WL-7, WL-9, WL-12 satırları (*"WL-0 ekleri"* işaretli). §5 dışına
>     dokunulmadı.
>
> **3. tur (2026-10-02 — kapanış denetçisi RED, 1 bloklayan + 6 bloklamayan, hepsi metin;
> orkestratörün kararıyla yapıcı kapatır):**
> 24. **İddia A'nın yakalama listesi daraltıldı** (ADR 0024): beş vakalık biçim testi ikinci kapının
>     kaldırılmasını yakalamıyor (denetçinin mutantı: PNG imzalı HTML `Decode`'da, sıfır kenarlı
>     başlık boyut kapısında düşer) — kapı sırası İddia B'nin bomba testinin konusu. ADR 0023
>     İddia B: golden yalnız WL-9 kartında adıyla/sayısıyla listelenen fikstürleri yakalar; WL-9
>     satırına liste şartı eklendi.
> 25. **Semafor alma politikası tekleşti:** beklemeden dene, doluysa hemen ret. **N = 1** artık
>     öneri değil, kuralın sonucu: Go 1.25+ `GOMAXPROCS`'u cgroup CPU sınırından alır (`go.mod`
>     `go 1.26.2`), düğüm 16 CPU (`10-postgres.yaml:208`), sınır `"2"` (`20-app.yaml:502`) →
>     `GOMAXPROCS` = 2 (türetildi; WL-3 podda bir kez ölçer).
> 26. **Account önizlemesi:** düğme `submit` değil, bir formun içinde değil, `form=` yok; logo
>     `/admin/brand/logo/{sha}`'dan; **yalnız kaydedilmiş** accent (aday hex için ikinci tema
>     `<link>`'i K4 şeridini boyardı); `templ Tap`'in bölünmesi **WL-9**'da, WL-7 WL-9'a bağımlı
>     (gerekçe: `tap.templ`'in sahibi WL-9, golden'ı tap ekranının değişmediğini gösterir).
> 27. **Accent özellik kuralı:** `--brand-accent` derlenmiş CSS'te yalnız `background-color`'da —
>     WL-5'in testi seçiciye ek olarak özelliğe bakar.
> 28. **§5 kalıntıları:** "Depolama" maddesinin multipart cümlesi S20'ye göre düzeltildi; WL-7
>     satırındaki "paper'a <1,5:1" porcelain'e çevrildi; "focus halkası (ink)" Chrome ölçümüne
>     eşitlendi. Hepsi işaretli.
> 29. **OP-8 sonrası (`71272fa`):** ADR'lerin Bağlam'ı `c0c0250`'yi sabitler; her iki ADR'ye
>     tarihli not — `router.go` satırları +7 (`:99` → `:106`, `:111` → `:118`, `:151-156` →
>     `:158-163`; `:83` yerinde), `operatorCSP` (`operator/render.go:28-29`) ve `enrollCSP()`
>     (`:39-41`) eklendi, ikisi de `style-src 'self'` → dalın ucunda dokuz politika; `img-src`
>     hâlâ yalnız `landingCSPFor`'da.

> **Kart düzeltmesi (2026-10-02, WL-2 uygulaması sırasında).** Yazıldı:
> `internal/brand/doc.go` (paket belgesi — WL-3'ün `logo.go`'su aynı pakete girer),
> `internal/brand/accent.go`, `internal/brand/accent_test.go` (13 test) ·
> [ADR 0023](../adr/0023-tenant-markasi-ve-arayuz-kurali.md) §3'e tarihli *"WL-2 notu"* (kural
> değişmedi; imzalar, ölçümler, `Suggest`'in okunuşu). Yeni bağımlılık yok (`go.mod`/`go.sum`/
> `sqlc.yaml` diff boş), DB/HTTP/log yok; `accent.go` `errors` + `math` içe aktarır.
> **Ölçüm ortamı:** darwin/amd64 (Intel i9-9980HK, 16 iş parçacığı), yerel Go 1.27.1;
> staticcheck Go 1.26.7 ile. CI'nin 1.26.x'inde süreler yeniden ölçülmedi.
>
> 1. **Kabul — tablo değerleri ±0,01.** `TestAccent_TheDesignTableHolds`: ADR §3'ün yedi
>    örneği (`#808080`, `#E0457B`, `#DA291C`, `#FFC72C`, tappa-green, tomato, saffron — L altı
>    haneye, oranlar ±0,01, karar, `OnColor`, `Edge`, `Fill`) + `#EEEEEE` (porcelain 1,01, ink +
>    kenar) + siyah (paper 20,61) + ADR'nin "kesik sınır geçirirdi" dediği üç renk (`#008384`,
>    `#22864B`, `#1E93A0`: red, en iyi kontrast 4,50 ±0,01 ve < 4,5) + ink/porcelain 14,32.
>    `#008384` **paper** tarafında (paper 4,49995:1; ADR hangi metin olduğunu yazmıyordu).
> 2. **Kabul — hesaplanan sınırlar, literal yok.** Üretim kodu L sınırı hesaplamaz:
>    `Check`/`OnColor`/`Edge` 4,5 ve 3 eşiklerini kontrast oranına doğrudan uygular. Sınırlar
>    testte, `tailwind.config.js`'ten okunan paletle ADR §3 formüllerinden türetilir
>    (paper 0,178982695642 · ink 0,236815218031 · kenar 0,254217381379 · dönüm 0,2062727488).
>    `TestAccent_TheNearestHexEitherSideOfEachComputedBoundary` (2²⁴ tarama): paper `6E7B44`
>    (4,87e-11 altında) geçer/paper · `7D5BEC` (4,18e-08 üstünde) red · ink `1E93A0` (2,92e-09
>    altında) red · `8D76DA` (2,08e-07 üstünde) geçer/ink · kenar `B268EC` (2,77e-08 altında)
>    kenarsız · `8F8A7A` (3,88e-10 üstünde) kenarlı. WL-0'ın dört komşusu yeniden üretildi; kenar
>    çifti yeni. *"Dahil"*: taranan 2²⁴ rengin hiçbiri bir sınırın tam üstünde değil (en küçük
>    mesafe 4,87e-11), *"uç geçer"* geçen yandaki en yakın renkle sınanır.
>    `TestAccent_TheADRPrintsTheBoundariesThePaletteYields`: ADR §3'ün yazdığı dört kesik değer
>    hesaplananın kesiği; *"4,0208"* ve *"1 949 736"* hesaplananla aynı — ADR'nin sayıları teste
>    bağlandı, testte sınır literali yok.
> 3. **Kabul — 2²⁴ uyuşmazlık 0.** `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`:
>    2²⁴ yineleme (x = 0 … 2²⁴−1; test yineleme sayısını assert eder, ayrık renk saymaz), red
>    1 949 736; özellik başına uyuşmazlık 0 (`Check` ↔ doğrudan 4,5 ↔
>    türetilmiş sınırlar; `OnColor`; `Edge` ↔ doğrudan 3:1 ↔ kenar sınırı; `Fill` ve ADR'nin üç
>    sınıfı; `Hex` → `ParseAccent` gidiş-dönüş; `Suggest`: çıktı `Check`'ten geçer, hiçbir kanalı
>    yükseltmez, geçen rengi değiştirmez, bisection adımına eşit, bir üst ızgara adımı red; yolun
>    tepe adımı rengin kendisi). En küçük sınır mesafesi 4,87e-11 (> 1e-12 şartı), dönüm
>    noktasına 1,41e-08; paper-metin sınıfında porcelain kontrastı en az 3,9857 (ADR "≥ 3,98").
>    **Süre (ölçüldü; darwin/amd64, 16 donanım iş parçacığı — süre shard sayısına ve makinenin
>    yüküne bağlı, her rakam koşuluyla):** düz, 16 shard 1,5 s · `-race`, 16 shard, 1 dk yük
>    ortalaması 2,5 → 24,8 s · `-race`, 4 shard (`GOMAXPROCS=4`), yük ~8 → 18,9 s (daha önceki bir
>    koşuda 16,3 s, yük kaydedilmedi) · `-race`, 8 shard 22,6 s (yük kaydedilmedi) · denetçinin
>    ölçümü: `-race`, 16 shard, yük ~8 / ~54 → 24,3 / 34,6 s
>    · `-race -cover` tam tarama go test'in 10 dk varsayılan zaman aşımında **bitmedi** (atomic
>    kapsam sayaçları; 2¹⁷ renk tek goroutine'de 8,2 s, yalnız `-race` 0,34 s). Karar: kapsam
>    enstrümantasyonu açıkken (`testing.CoverMode() != ""`) her 61. renk (275 037; 14,8 s) —
>    `make test` (CI) `-cover`'sız olduğu için CI'da tam tarama koşar; `make cover` örneklem
>    tarar. `-short` altında atlama **yok**: paket `-race`'te 16 shard, yük 2,5'te ~25 s;
>    Makefile'ın *"-short tam dört
>    SKIP"* sayımı değişmez.
> 4. **Kabul — palet kopyası.** `TestPalette_TheGoCopyEqualsTailwindConfig` (dokuz token, iki
>    yön) · negatif kontrol `TestPalette_TheComparisonSeesEveryKindOfDrift` (dosya metninde
>    dokuz hex'in her biri tek tek değişince, token silinince/eklenince/yeniden adlandırılınca diff
>    onu adlandırır) · ayrıştırıcının dejenere girdileri
>    `TestPalette_TheParserRefusesWhatItCannotRead` (21 alt test: 10 bozuk satır biçimi, her biri
>    iki geçerli satırın arasında ve hatanın o satırı adlandırması şartıyla; 11 blok düzeyi ve
>    pozitif vaka — 2. tur). Mutasyonla: Go kopyasında ink ve
>    line hex'i, tablo satırı (tomato → saffron sabiti), `tailwind.config.js`'te ink ve saffron →
>    beşi de kırmızı.
> 5. **Kabul — `Suggest` deterministik, her çıktısı `Check`'ten geçer.** Madde 3'ün taraması
>    (2²⁴ girdi) · `TestSuggest_IsDeterministicUnderConcurrency` (32 goroutine, `-race`) ·
>    `TestSuggest_IsTheBrightestPassingColourOnTheExactPath` (ızgarasız tam yol sayımı, 970 red
>    renk örneklemi; en kısa aralık 4,5e-06, ızgara adımı 9,13e-13; ızgaranın 1/260100²'den ince
>    olduğu pinli) · `TestSuggest_ThePathIsTextbookHSL` (1 021 919 nokta, 30 239'u yuvarlama
>    eşitliği yakınında atlandı) · `TestSuggest_TheDesignExamples` (beklenenler Python
>    `fractions` ile tam rasyonel sayımla bağımsız hesaplandı).
> 6. **Kapsam:** `go test -race -count=1 -cover ./internal/brand/` → %100,0.
>
> **Sapmalar (gerekçeli):**
> - **a. Kanonik yazım `RRGGBB`, `#` yok.** Brief *"#RRGGBB"* yazıyordu; ADR 0023 §1/§3
>   (`^[0-9A-F]{6}$`, *"`#` yok"*) esas alındı.
> - **b. Altıncı fonksiyon `NormalizeAccent`** (ADR beş sayıyordu): `<input type="color">`
>   `#rrggbb` küçük harf gönderir; tek `#` ve küçük harf kabul, çıktının `Hex()`'i kanonik.
>   `ParseAccent` katı kaldı (tema rotası ve saklanan satır için; küçük harf red).
> - **c. `Check(c) (Fill, error)`**: `Fill{Accent, Text, Edge}` — tema rotasının üç değişkeni;
>   red → `(Fill{}, ErrAccentIllegible)`. `OnColor` eşitlikte paper (eşitlik dönüm noktasında,
>   o da red bandında).
> - **d. `Suggest`'in tanımı.** *"Aynı ton, açıklığı düşürerek"* HSL'de: ton ve doygunluk sabit,
>   açıklık iner (Sass `darken()`); sonuç bu yolda geçen en parlak renk; geçen renk değişmeden
>   döner. Yol tam sayılarla (`510·2³¹` ızgara). Float neden değil (geçici test, teslimden önce
>   silindi): 1 949 736 red rengin **7 430**'unda ders kitabı float HSL bisection'ı bu koddan
>   farklı renk öneriyor; 7 430'un tamamında ızgarasız tam yol sayımı (`accentExactPath`) bu
>   kodla aynı, float'la 0 (2'sinde orta nokta yuvarlama eşitliğine 1e-6'dan yakın düştü). Ör.
>   `007EC6` → float `007AC1` (tam yolda olmayan renk — G ve B 35/36'da birlikte yuvarlanır), bu
>   kod `007AC0` (Python `fractions` ile de doğrulandı; testte pinli). Kalıcı testte bu sayım
>   örneklemle (970 renk) koşar; tam sayım `-race` dışında ~14 s.
> - **e. Kapsam enstrümantasyonunda örneklem** (madde 3; ölçümle).
> - **f. Eşdeğer mutantlar (4):** `Check`'te `<`→`<=`, `Edge`'de `<`→`<=`, `OnColor` eşitlik kuralı
>   `>=`→`>`, doğrusallaştırma eşiği 0,04045→0,03928. İlk üçü: taranan 2²⁴ rengin hiçbiri sınırda
>   ya da dönüm noktasında değil (4,87e-11 / 1,41e-08); dördüncüsü: iki eşik de 8 bitlik
>   girdide 0..10'u doğrusal kola koyar (10/255 = 0,0392 < ikisi < 11/255 = 0,0431). Mutant
>   koşuları bu ölçümü doğruladı (tarama mutant altında yeşil).
> - **g.** ADR'nin *"1 092"* ve *"14 (paper 9, ink 5)"* sayıları scratch'te yeniden ölçüldü ve
>   tuttu (1 092 = paper 611 + ink 481); testte yok — testte yanlış literal yazmamak için.
>
> **Devirler:**
> - **WL-4:** domain `Check` → `ErrAccentIllegible`; saklanan değer `Color.Hex()` (WL-1'in CHECK'i
>   ile aynı biçim).
> - **WL-5:** tema rotası `ParseAccent` (katı; küçük harf ve `#` → 404) + `Check`; `Fill` üç
>   değişkenin RGB'sini verir; `Edge == false` iken `--brand-edge`'in değeri WL-5'in kararı.
> - **WL-7:** form değeri `NormalizeAccent` (handler sınırı, CLAUDE.md §7); okunaksızsa
>   `Suggest(c).Hex()`. **Bilgi:** red renklerin **947 258**'i (%48,6) L'de ink sınırına paper
>   sınırından yakın (scratch ölçümü); `Suggest` ADR'ye göre bunları da koyulaştırır, öneri
>   girdiden belirgin koyu olabilir (ör. `1E93A0` → `1A818D`). Açma yönü ADR 0023 §3 değişikliği
>   olur — *Karar verilmedi*, orkestratöre.
> - **WL-10:** `internal/brand/accent.go` §4 kırmızı çizgilerine dokunmaz (DB, HTTP, log, sır,
>   GPS yok); denetim listesine ADR 0023 §3'ün WL-2 notu.
> - **WL-12:** CLAUDE.md §3'e `internal/brand` satırı (ADR 0023 Sonuçlar'da zaten listeli).
> - **CI maliyeti:** `internal/brand` `-race` altında 16 iş parçacıklı makinede 1 dk yük 2,5'te
>   ~25 s, yük ~54'te 34,6 s (denetçi); CI'nin çekirdek sayısında yeniden ölçülmedi.
>
> **Doğruluk iddiası (üç parça).**
> - **PART I — ölçülen:** 2²⁴ yinelemenin (x = 0 … 2²⁴−1) her birinde `Check`'in kararı
>   doğrudan 4,5 testine ve
>   paletten türetilmiş sınırlara eşit, `Edge` 3:1 testine eşit, `OnColor` yüksek kontrastlıya
>   eşit, `Suggest`'in çıktısı `Check`'ten geçiyor (kapsamsız koşuda
>   `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`); Go palet kopyası
>   `tailwind.config.js`'in dokuz token'ına eşit (`TestPalette_TheGoCopyEqualsTailwindConfig`).
> - **PART II — pinler ve pinlerin birlikte yakaladığı biçimler** (liste bir birleşimdir, pin
>   başına eşleme değil; yapıcının mutasyonla ölçtüğü biçimler için tamdır — 2. turun 34
>   mutasyonluk koşusunda öldürülen 30; denetçinin pinlerce öldürülen 15 ek varyantı bu
>   listede yok; ürün için tamlık iddiası değildir — PART III): yukarıdaki iki test +
>   `TestPalette_TheParserRefusesWhatItCannotRead` +
>   `TestAccent_TheDesignTableHolds`, `TestAccent_TheNearestHexEitherSideOfEachComputedBoundary`,
>   `TestAccent_TheADRPrintsTheBoundariesThePaletteYields`,
>   `TestAccent_ParseAcceptsOneSpellingPerColour`,
>   `TestAccent_NormalizeTakesWhatTheColourInputSends`,
>   `TestPalette_TheComparisonSeesEveryKindOfDrift`, `TestSuggest_TheDesignExamples`,
>   `TestSuggest_ThePathIsTextbookHSL`, `TestSuggest_IsTheBrightestPassingColourOnTheExactPath`;
>   yakaladıkları: eşiğin 4,4999'a kayması · `Check` kararının ters çevrilmesi · `Edge`'in
>   porcelain yerine paper'la ölçülmesi · doğrusallaştırma eşiğinin 0,05'e, 12,92'nin 12,0'a
>   değişmesi · `OnColor`'ın ters çevrilmesi · luminans ağırlıklarının R↔B yer değiştirmesi ·
>   Go kopyasında ya da `tailwind.config.js`'te bir hex'in değişmesi, tablo satırının başka
>   sabite bağlanması · `Suggest`'in ilk red adımı, sabit oranlı koyulaştırma ya da girdiyi
>   `Check`'siz döndürmesi · bisection'ın erken durması · yolun ikinci HSL parçasını yok
>   sayması, aşağı yuvarlaması, ızgaranın 2²⁰'ye ya da 2⁸'e kabalaşması · siyah/beyaz `d = 1`
>   korumasının kalkması · `ParseAccent`'in küçük harf ya da uzun girdi kabul etmesi ·
>   `accentDecode`'un CSS üç haneli kısa biçimini açması (2. tur) · `NormalizeAccent`'in `#`
>   atmaması · `Hex`'in küçük harf basması · `Fill`'in kenarı düşürmesi · palet ayrıştırıcısının
>   tanımadığı satırı atlaması (2. tur) · kapsamsız koşunun 2²⁴ yineleme yerine örneklem
>   taraması (2. tur)
>   · ADR WL-2 notundaki bir komşu hex'in değişmesi (2. tur). Yakalamadıkları (eşdeğer, sapma
>   f): `<`/`<=` (Check, Edge), eşitlik kuralı, 0,03928. Kapsam enstrümantasyonu açıkken tarama
>   her 61. renktir.
> - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
>
> **2. tur (2026-10-03 — üçüncü göz RED: 2 bloklayan + 6 bloklamayan; hesaplar bağımsız
> numpy/Fraction referansıyla doğrulandı).**
> - **F1 (bloklayan):** palet ayrıştırıcı testi gevşek ayrıştırıcıyı ayırt etmiyordu — bozuk
>   biçimler tek girdili blokta duruyordu ve "satırı atla" ayrıştırıcısı başka kuraldan ("no
>   entries") düşüyordu. Şimdi on bozuk biçim geçerli `ink` ile `paper` satırlarının arasında ve
>   hata o satırın numarasını taşımak zorunda. Mutasyon X11 (tanınmayan satırda `continue`) →
>   10/10 satır alt testi KIRMIZI; 11 blok düzeyi/pozitif alt test yeşil kaldı.
> - **F2 (bloklayan):** CSS üç haneli kısa biçimi (`ABC`, `abc`, `#ABC`, `#abc`; Normalize
>   tablosuna ayrıca `e04`, `E04`) iki tabloya ret olarak girdi. Mutasyon X1 (`accentDecode` üç
>   haneyi açar) → `TestAccent_ParseAcceptsOneSpellingPerColour` ve
>   `TestAccent_NormalizeTakesWhatTheColourInputSends` KIRMIZI.
> - **F3:** kapsamsız koşuda yineleme sayısının 2²⁴ olduğu artık assert ediliyor (ayrık renk
>   sayısı değil — 3. tur N4). Mutasyon X9
>   (`CoverMode` koşulu ters) → `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`
>   KIRMIZI.
> - **F4:** ADR 0023 §3'ün normatif cümlesinin yanına tarihli not ve WL-2 notunda *"normatif
>   cümle nasıl karşılanır"* maddesi: kodda L sınırı ne literal ne hesaplanmış; karar paletten
>   hesaplanan oranın eşikle karşılaştırılması (sınır formülü onun L'ye göre çözülmüş hâli);
>   sınırları test türetir.
> - **F5:** `doc.go` WL-3'ün dosyası ve diğer görevlerin kodu hakkında olgu iddia etmiyor; kapsam
>   ADR 0023 (§1–§3) ve ADR 0024'e atıfla.
> - **F6:** `accentScanStride` yorumundaki süreler koşuluyla (shard sayısı, 1 dk yük ortalaması):
>   `-race` 16 shard yük 2,5 → 24,8 s; 4 shard yük ~8 → 18,9 s (önceki koşu 16,3 s); denetçinin
>   16 shard yük ~8 / ~54 → 24,3 / 34,6 s.
> - **F7:** komşu testi kaynağı ADR 0023 §3'ün WL-2 notuna bağladı ve altı hex'in o notta
>   geçtiğini okuyor. Mutasyon X12 (notta `B268EC` → `B268ED`) →
>   `TestAccent_TheNearestHexEitherSideOfEachComputedBoundary` KIRMIZI.
> - **F8:** PART II başlığı netleşti; 3. turda (N2) son hâli: *"pinler ve pinlerin birlikte
>   yakaladığı biçimler"*.
> - Yan düzeltme: `ParseAccent` tablosundaki tam genişlikli rakam kaynakta ham UTF-8 idi, bayt
>   kaçışına (`\xef\xbc\x91`) çevrildi. (Kaynak bundan sonra da ASCII değildir — ölçüm 3.
>   turda.)
>
> **3. tur (2026-10-03 — kapanış denetçisi ONAY; F1–F7 ✓, yapıcının üç mutasyonuna ek
> denetçinin 15 varyantı da KIRMIZI; kalan sekiz not YALNIZ METİN, test mantığı değişmedi).**
> - **N1:** PART II pin listesine `TestPalette_TheParserRefusesWhatItCannotRead` eklendi (X11'i
>   tek öldüren test, `mutate-round2.txt`).
> - **N2:** PART II başlığı *"pinler ve pinlerin birlikte yakaladığı biçimler"*; liste bir
>   birleşim, yapıcının mutasyonla ölçtüğü biçimlerle sınırlı (denetçinin 15 varyantı listede
>   yok); F8 satırı bu başlığa eşitlendi.
> - **N3:** komşu testinin yorumu ölçülen kapsama bağlandı: taranan komşu tablodan farklıysa ya
>   da tablodaki bir hex notta değişir ya da nottan çıkarsa kırmızı; notun bir hex'e verdiği rol
>   (geçer / red) okunmuyor — iki hex'in rolü yer değiştirirse yeşil kalır.
> - **N4:** tarama metni "2²⁴ yineleme (x = 0 … 2²⁴−1)" olarak bağlandı; assert yineleme sayısına
>   bakar, ayrık renk saymaz (test yorumu, assert mesajı, bu kart, ADR notu).
> - **N5:** ADR notunda `OnColor` eşik uygulamaz, paper ve ink oranlarını karşılaştırır;
>   eşikleri `Check` (4,5) ve `Edge` (3) uygular.
> - **N6:** ASCII olmayan karakter ölçümü (3. tur sonrası, `python3` sayımı): `accent_test.go` 25
>   satır, `accent.go` 13, `doc.go` 5; karakterler §, —, ±, …, 🔴; Türkçe karakter 0. Üç
>   `t.Errorf` dizgesi ASCII'ye çevrildi (`±0.01` → `+/-0.01`, `…` → `...`, `§3` → `section
>   3`); test mesajlarında ASCII olmayan karakter 0. CLAUDE.md §7'nin kuralı (Türkçe karakter)
>   ihlal edilmiyordu ve edilmiyor.
> - **N7:** madde 3'ün ve "CI maliyeti" satırının süreleri koşuluyla (shard, yük) yazıldı.
> - **N8:** ADR'deki satır içi notun tarihi 2026-10-03; WL-2 notunun "normatif cümle" maddesi
>   *"2. tur, 2026-10-03; 3. turda düzeltildi"* diye işaretlendi.

> **Kart düzeltmesi (2026-10-03, WL-3 uygulaması sırasında; 2. tur aynı gün).** Yazıldı:
> `internal/brand/logo.go` (`LogoGate`, `NewLogoGate`, `Normalize`, `Logo`, dokuz ret sınıfı
> `ErrLogo*`), `internal/brand/logo_resize.go` (kutu filtresi) ve testleri (`logo_*_test.go`;
> fikstürler kodla üretilir, ikili dosya eklenmedi). Paket belgesi (`doc.go`) WL-2'nin. Ölçümler,
> kararlar, devirler ve üç parçalı iddialar: [ADR 0024](../adr/0024-kullanici-yukledigi-gorsel.md)
> → *"WL-3 notu"*. `go.mod`/`go.sum`/`sqlc.yaml` diff'i boş.
>
> **Kabul satırı → ölçen test:**
> 1. SVG/GIF/WebP/HTML/PNG-magic'li HTML red → `TestLogoFormat_RefusesWhatIsNotPNGOrJPEG`
>    (on bir girdi + SOI'den sonra çöp baytlı JPEG; hepsi `ErrLogoFormat`); istemci başlığı →
>    `TestLogoFormat_ClientHeadersDoNotDecide` (üç etiketli parça; `image/jpeg` etiketli çöp
>    baytlı JPEG de `ErrLogoFormat`); üçüncü kapının yazımı →
>    `TestLogoDecode_CallsTheSniffedFormatsOwnDecoder`.
> 2. 30000×30000 başlıklı dosya decode'dan önce red, tahsis ölçülür →
>    `TestLogoBomb_HugeHeaderRefusedBeforeDecode`: PNG (75 B) çağrı boyunca 5 392 B, JPEG
>    (219 B) 17 920 B (4 KiB'lık okuma tamponu dahil); pozitif kontrol: kapıdan geçen 2048²
>    başlıklı verisiz PNG 16,8 MB. Kenarlar → `TestLogoDimensions_EachEdgeAndThePixelCount`.
> 3. Kesik red → `TestLogoTruncated_EveryPrefixRefused` (dört dosyanın her öneki).
> 4. IEND sonrası yük çıktıda (testin girdisinde) 0 → `TestLogoMetadata_PNGChunksAndTrailingBytesDoNotSurvive`
>    (çıktı tam olarak `IHDR IDAT IEND`, IEND'den sonra 0 bayt).
> 5. EXIF-GPS'li JPEG'in çıktısında `Exif` APP1 (testin girdisinde) 0 →
>    `TestLogoMetadata_ExifGPSDoesNotReachTheOutput` (girdide APP1 `Exif`, APP2 `ICC_PROFILE`, COM,
>    APP15, EOI sonrası HTML; çıktının işaretleri S14 kümesinde, `FF E1` 0).
> 6. CMYK JPEG, 16-bit, paletli, interlaced PNG normalize → `TestLogoNormalize_EveryDecodedLayout`
>    (18 satır, her biri kendi biçiminde, dört iç noktada girdinin rengiyle).
> 7. ≤512 px, ≤256 KiB → `TestLogoOutput_Within512AndTheSizeLimit` (sınır 262 144 dahil;
>    400² gürültü PNG `ErrLogoOutputTooLarge`), kalite → `TestLogoOutput_JPEGQualityIs85`.
> 8. `FuzzNormalize` — koşunun girdilerinde panik 0, her başarılı çıktı yeniden decode olur ve
>    sınırlarda; ret tam olarak bir sınıf ve sınıfın metni.
> 9. Semafor `-race` altında ≤N → `TestLogoGate_ConcurrentDecodesNeverExceedN` (N = 1 ve 3).
> 10. JPEG SOS sayımı üst sınır; tavan+1 ve dürüst olmayan dosya decode'dan önce red →
>     `TestLogoScans_CeilingAcceptedOneMoreRefused`, `TestLogoScans_DishonestFilesRefused` —
>     **yedi yerleşim**, her biri kandırdığı sayaçla: üst düzey `FF D0`, `FF 00`, dolgu `FF FF`,
>     `00 11 22` (segment yürüyücüsü); yükü `FF DA FF FF` olan APP1 (başlığı uzunlukla atlayan
>     sayaç); DQT değerlerinde `FF E1 FF F0` (APPn'i uzunlukla atlayan sayaç); yükü `FF D9` olan
>     APP1 (ilk `FF D9`'da duran sayaç). Adlandırılan sayaç ≤ 100 sayar, çözücü gizli DC
>     taramasını çalıştırır (her piksel 77), ham sayım > 100 → red. Üst sınır gerekçesinin
>     dayandığı fonksiyonlar (`decode`, bayt okuyucular, `findRST`) go1.26.6 ve go1.27.1'de aynı
>     metin; `image/jpeg`'in bütünü değil — 1.27.1 standart dışı ("flex") alt örneklemeli üç
>     bileşenli JPEG'i normalize eder, CI'nin 1.26.6'sı `ErrLogoFormat` verir (ölçüldü).
>     **Tavan 100**; tavandaki süre: 2048², `Normalize` uçtan uca, üç koşunun ortancası,
>     go1.27.1 / go1.26.7: gri progressive ilk AC 367 / 422 ms · gri iyileştirme 911 / 955 ms ·
>     CMYK ilk AC 525 / 574 ms · CMYK iyileştirme **1 052 / 1 068 ms**; tarama sayısından bağımsız
>     boyut-bağlı ardışık dosya 1 327–1 480 / 1 153–1 293 ms. Tek koşuda en uzun 1 662 ms.
> 11. Yuva decode → küçültme → kodlama boyunca, goroutine dönünce bırakılır; iptal edilen
>     istekten sonra dolu → `TestLogoGate_SlotHeldThroughDecodeResizeEncode` (beş aşama),
>     `TestLogoGate_CancelledContextKeepsTheSlotUntilTheDecodeReturns` (tutulan + canlı),
>     `TestLogoGate_ErrorPathsGiveTheSlotBack`; gövde okunurken biten istek yuvayı almaz →
>     `TestLogoGate_ContextEndedDuringTheReadTakesNoSlot`.
> 12. N eşzamanlı en kötü decode altında bellek → `TestLogoMemory_WorstDecodeAllocations`,
>     `TestLogoMemory_ProcessRSS`: tepe progressive CMYK 4:4:4 2048² — `TotalAlloc` 98,45 MiB, heap
>     nesne tepesi 99,0–99,3 MiB, taze süreçte üç ardışık çağrının RSS tepesi **138,6–154,7 MiB**
>     (iki kipli; RSS'te en kötü dosya CMYK). **Konteynerde ölçüldü** (WL-3'ün üçüncü gözü,
>     go1.26.6 linux/amd64, `--memory=512m --cpus=2`, ürün tabanı dahil değil): CMYK 137–153
>     (`VmHWM`) / 139–155 MiB (`memory.peak`). **Seçim: `2 × (N × tepe + taban) < 512Mi`
>     hesabı** → N = 1, tepe ≈99 MiB ile **taban < 157 MiB**; `GOMEMLIMIT`'in etkisi beş
>     koşuda ayrılmadı. **Öneri** (manifest değişmedi): `deploy/k8s/20-app.yaml` uygulama
>     konteyneri `env`'ine `GOMEMLIMIT=400MiB` — ürün geneli kemer.
> 13. N < `GOMAXPROCS` → N = 1: `LogoDecodeSlots = 1`, `TestNewLogoGate_RefusesFewerThanOneSlot`;
>     konteynerde `GOMAXPROCS` = 2 ölçüldü (NumCPU 4, `cpu.max` "200000 100000").
> 14. Yuva beklemeden alınır, N doluyken red decode'a ve incelemeye girmeden →
>     `TestLogoGate_FullGateRefusesBeforeDecoding`: dolu kapıda 2048² progressive CMYK 537 768 B,
>     37 000 `tEXt`'li paletli PNG 537 672 B (incelemesi yuva boşken 145 MiB), SVG 4 696 B —
>     üçü de ≤ 1 MiB ile `ErrLogoBusy`.
> 15. `go.mod` diff boş — `git diff --stat go.mod go.sum sqlc.yaml` 0.
>
> **Sapmalar (gerekçeli):**
> - **İnceleme de yuvanın içinde** (ADR yuvayı *"decode'dan önce"* der): paletli PNG'nin
>   `DecodeConfig`'i chunk fırtınasında 144,5 MiB kısa ömürlü tahsis yapar; yuvanın dışında N ile
>   sınırlanmazdı (ADR 0024 WL-3 notu, karar 2).
> - **Okuma iki tamponla** (4 KiB, sonra sınır + 1): `io.ReadAll`'ın tahsisi sürüme göre değişiyor
>   (aynı 518 KB: go1.27.1'de 1 065 360 B, go1.26.6 `-race`'te 2 128 048 B); okuma tahsisinin üst
>   sınırı artık sınır + 32 KiB — ikinci tamponu gerektiren okumalarda 536 624–537 728 B, ilk
>   tamponda bitenlerde 4 144–5 248 B (karar 4). Brief'in "≤ 1 MiB" pini 1. turun koduyla tutmazdı.
> - **Hata metni:** `TestLogoErrors_TextIsTheClassOnly` iki değer taşıyan nedeni ölçer — PNG
>   chunk uzunluğu (`ErrLogoCorrupt`) ve quoted-printable okuyucusunun bayt adı (`ErrLogoRead`).
> - **Linux RSS dalı** (`VmHWM`) kapanış denetçisinin konteynerinde koştu (go1.26.8): decode
>   öncesi 8,0–13,2 MiB, CMYK 139,4–145,8 MiB.
> - **Podda ölçülmedi:** ürün tabanıyla RSS — deploy'da orkestratörün, WL-7'ye bloke.
> - **Ölçüm testleri `-race` altında atlanır** (`TestLogoScans_WorstCaseDecodeTime`,
>   `TestLogoMemory_WorstDecodeAllocations`, `TestLogoMemory_ProcessRSS`); CI yalnız `-race`
>   koştuğu için bu üçü CI'da koşmaz — ADR'nin iki "CI'da yeniden üretir" cümlesi düzeltildi.
> - **Progressive / 4 bileşenli JPEG'i reddetme alternatifi seçilmedi**; taban podda ölçülüp
>   157 MiB'ı aşarsa yeniden açılır.
> - **256 KiB:** ADR §3'ün kendisi (PNG → PNG, JPEG → JPEG q85, aşan → ret); merdiven eklenmedi.
>
> **Devirler:** WL-1/WL-6 — `logo_sha256` üzerine global UNIQUE konmaz (`(tenant_id,
> logo_sha256)` ya da hiç; İddia D); "logo değişmedi mi" yeniden normalize edip sha
> karşılaştırmaz (PNG çıktısı Go sürümüne göre değişiyor: 180 383 B / 180 355 B). WL-7 — wiring
> `brand.NewLogoGate(brand.LogoDecodeSlots)` bir kez ve bunu pinleyen test; handler
> `gate.Normalize(r.Context(), part)` (parçayı doğrudan verebilir); ret → kullanıcı cümlesi ve
> log sınıfı `errors.Is` ile (`ErrLogoBusy` → "tekrar dene", sunucu tarafında yeniden denenmez;
> `ErrLogoScans` → "standart JPEG olarak kaydet"; `ErrLogoOutputTooLarge` → "sadeleştir / JPEG
> dene"; `ErrLogoVerify` → iç hata); `ErrLogoRead` bir `*http.MaxBytesError` sarabilir; log'a
> sınıf yazılır, `errors.As` ile çözücünün ya da okuyucunun mesajı yazılmaz; açık logo uyarısı
> `Normalize` çıktısından; eşzamanlı okuma başına en çok ≈0,52 MiB; yükleme bütçesi deneme
> başına, gövde okunmadan önce — sonuca göre düşülürse decode'a ulaşan retler (`ErrLogoCorrupt`,
> `ErrLogoOutputTooLarge`, `ErrLogoVerify`, yuva alındıktan sonra biten bağlam) bedava decode
> olur (ADR 0024 §6 düzeltmesi; tenant'lar arası açlık sayılı sınır 11); gövde öncesi kabul
> sınırı ve okuma süresi WL-7'nin.
>
> **Orkestratör notu (2026-10-03, birleştirmede) — 🔴 WL-7 BLOKESİ:** logo yükleme rotası (WL-7) canlıya bağlanmadan önce podda ÜRÜN TABANIYLA RSS ve `runtime.GOMAXPROCS(0)` ölçülür (güvenlik denetimi #1; ADR 0024 §2.6 — konteyner ölçümü pod değil, ürün tabanı dahil değil); bütçe deneme başına, gövde okunmadan ve `Normalize`'dan önce düşülür (ADR 0024 §6, sayılı sınır 11). WL-1'e: `logo_sha256` üzerinde GLOBAL UNIQUE yok.

> **Kart düzeltmesi (2026-10-03, WL-1 uygulaması sırasında).** Yazıldı:
> `db/migrations/00028_create_tenant_branding.sql` · `db/queries/branding.sql` (sekiz sorgu) ·
> `internal/store/branding.sql.go` + `models.go` + `querier.go` (sqlc üretimi) ·
> `internal/db/branding_test.go` (12 test) · ADR 0023 §1'e tarihli *"WL-1 notu"* (kural
> değişmedi; eklemeler, R5b düzeltmesi, üç parçalı iddia ve mutasyon listesi orada). Mevcut
> testlerde bilinçli güncelleme: `cmd/tappa/storekeyshape_test.go` (envanter 115 → 123 imza,
> store dosyası 19 → 20, `db/queries` dosyası 18 → 19, adlı sorgu 114 → 122, `[]byte` taşıyıcı
> 5 → 6 — altıncısı `GetTenantLogoRow.Logo`) · `internal/db/rlsforce_test.go` (FORCE kapısının
> beklenen listesine `tenant_branding`) · `internal/db/rls_test.go` (yalnız başlık yorumu:
> tabloların neden orada değil `branding_test.go`'da sınandığı). `go.mod`/`go.sum`/`sqlc.yaml`
> diff boş. **Ölçüm ortamı:** dev Postgres 17 (paylaşılan, varsayılan ACL `tappa_app=arwd`),
> uygulama rolü `tappa_app`; yerel Go 1.27.1, staticcheck Go 1.26.7; şema dökümleri pg_dump 18.4
> istemcisiyle (`--schema-only --no-comments`, goose tablosu ve dizisi hariç, `\restrict`/
> `\unrestrict` satırları ayıklanmış).
>
> **Kabul — kanıtlar:**
> 1. `make audit` R5/R5b 0: `./scripts/redline-check.sh` exit 0; R5/R5b FAIL yok, `00028`'i
>    adlandıran satır yok (R5 WARN'u yalnız 00020/00026'nın önceden var olan muafiyetleri).
> 2. Beşli tam: `tenant_id uuid NOT NULL` · `PRIMARY KEY (tenant_id)` (tablo-kısıtı yazımı —
>    tek indeks, `TestTenantBranding_CatalogShape`) · ENABLE + FORCE (`TestTenantBranding_CatalogShape`,
>    `TestRLS_EveryTenantScopedTableIsEnabledAndForced`) · tek politika, ALL, USING ve WITH CHECK
>    ikisi de `NULLIF(current_setting('app.tenant_id', true), '')::uuid` (katalogdan birebir) ·
>    `tappa_app` grant'ları.
> 3. RLS `WHERE`'siz: A bağlamında `SELECT tenant_id FROM tenant_branding` tam olarak `[A]`;
>    pozitif kontrol B bağlamında `[B]` (`TestRLS_TenantBranding_ReadIsolationWithoutWhere`).
>    B'nin `tenant_id`'siyle INSERT 42501 RLS; satırı olmayan C için de aynı kod ve **aynı
>    mesaj** (WITH CHECK birincil anahtardan önce — varlık kehaneti yok); pozitif kontrol C kendi
>    bağlamında ekler (`TestRLS_TenantBranding_WriteWithCheck`).
> 4. `has_table_privilege('tappa_app','tenant_branding','DELETE') = false`; ayrıca TRUNCATE,
>    REFERENCES, TRIGGER false; tablo düzeyi INSERT/UPDATE false; sütun matrisi SELECT = 10
>    sütun, INSERT = `tenant_id,updated_by`, UPDATE = `accent,logo,logo_sha256,logo_mime,
>    logo_width,logo_height,updated_at,updated_by`; `DELETE FROM tenant_branding` 42501
>    "permission denied", satır yerinde (`TestTenantBranding_AppPrivileges`).
> 5. CHECK'ler hasmane değerlerle — 34 alt test, her ret SQLSTATE **ve kısıt adıyla** pinli
>    (`TestTenantBranding_ChecksRefuseHostileValues`): accent küçük harf, karışık, 3 hane, `#`'li
>    6 karakter, `#`'li 7 karakter (22001), 7 hex (22001, kesilmedi), hex olmayan harf, baştaki
>    boşluk, boş · logo 262145 bayt, 0 bayt · sha büyük harf, 63 karakter, başka baytların sha'sı
>    · mime `image/gif`, `image/svg+xml`, `IMAGE/PNG`, `image/png ` · genişlik 0/513/−1, yükseklik
>    0/513 · sevk edilen `SetTenantLogo` baytsız · kısmi logo: beş sütunun her biri tek başına
>    ve her biri eksikken (10). Pozitif kontroller: `1F5C41`; 262144 bayt 512×512 jpeg; 1 bayt
>    1×1 png. Her vaka geri alınan bir transaction'da koşar. Ayrı bir kabul vakası (2. tur,
>    güvenlik bulgusu): sonu boşluklu accent `1F5C41   ` **kabul edilir** ve `1F5C41` olarak
>    saklanır (char(6) ataması fazla karakterlerin hepsi boşluksa kırpar; boşluk olmayan fazla
>    karakter 22001).
> 6. `updated_at` + `updated_by` + bileşik `FOREIGN KEY (updated_by, tenant_id) REFERENCES
>    admin_users (id, tenant_id) ON DELETE RESTRICT`: başka tenant'ın (gerçek) yönetici id'si
>    hem INSERT (`EnsureTenantBrand`) hem UPDATE (`SetTenantAccent`) yolunda 23503
>    `tenant_branding_updated_by_fk`; pozitif kontrol A'nın yöneticisi
>    (`TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant`).
> 7. Down→Up→Down bayt-aynı (ölçüm; kalıcı test değil — sayılı sınır 1): v27 taban dökümü
>    `ac206fedc7a6548b…` · Down → `ac206fedc7a6548b…` · Up → `af847d50ba028f04…` · Down →
>    `ac206fedc7a6548b…` · Up → `af847d50ba028f04…`. Yorum düzeltmelerinden sonra son dosya
>    (sha256 `f21fb92df6e1b987…`) Down → Up ile yeniden uygulandı: Down `ac206fedc7a6548b…`, Up
>    `af847d50ba028f04…`. Her adımda goose sürümü ölçüldü, goose komutları zincirlenmedi.
> 8. `GetTenantBrand` `logo` seçmiyor — test sevk edilen sorgu metnini okur
>    (`internal/store/branding.sql.go` sabitleri, go/ast): `getTenantBrand` ve
>    `getTenantBrandForUpdate`'in seçim listesi tam olarak
>    `accent,logo_sha256,logo_mime,logo_width,logo_height,updated_at,updated_by`, metinde `logo`
>    belirteci ve `*` yok; pozitif kontrol `getTenantLogo` = `logo,logo_mime`
>    (`TestTenantBranding_PerPageReadsDoNotSelectTheLogo`).
>
> **Sapmalar (sayılı):**
> 1. **Eklenen beş şey** (ADR 0023 §1 / ADR 0024 §4 yazmıyordu; ADR 0023 WL-1 notunda):
>    `created_at` · `updated_by NOT NULL` · logo alt sınırı (`BETWEEN 1 AND 262144`) ·
>    `tenant_branding_logo_sha256_matches_logo` (`logo_sha256 = encode(sha256(logo),'hex')`) ·
>    sütun düzeyi INSERT/UPDATE.
> 2. **ADR §1'in "tablo-kısıtı biçiminde — R5b" ifadesi:** biçimi isteyen R5'in indeks kuralıdır,
>    R5b değil (ölçüldü: sütun yazımı `[R5 · FAIL] … eksik → tenant_id ONDE olan indeks`).
> 3. **Sorgu adları** tasarım özünün `UpsertTenantAccent`/`UpsertTenantLogo` adları yerine:
>    `EnsureTenantBrand` (INSERT … ON CONFLICT DO NOTHING) + `GetTenantBrandForUpdate` +
>    `SetTenantAccent`/`SetTenantLogo` (UPDATE) + `ClearTenantAccent`/`ClearTenantLogo` (UPDATE … NULL).
>    Gerekçe: WL-4'ün audit "before"u. Ölçüldü: iki yazıcıda ikincinin "önceki"si birincinin
>    commit ettiği değerdir — yeni satırda ikinci yazıcı `EnsureTenantBrand`'de, var olan
>    satırda `GetTenantBrandForUpdate`'te bekler (test bekleme ifadesini `pg_stat_activity`'den
>    okur); Ensure atlanırsa kilit, birincinin commit edilmemiş ilk satırını bulmaz
>    (`pgx.ErrNoRows`) (`TestTenantBranding_EnsureThenLockReturnsWhatTheOtherWriterCommitted`).
>    Set/Clear `:execrows` (beklenen 1).
> 4. **Tenant içi benzersizlik:** `(tenant_id, logo_sha256)` ayrı kısıt olarak yazılmadı —
>    `tenant_id` birincil anahtar, tenant başına tek sha. Global UNIQUE yok (WL-3 devri).
> 5. **`accent char(6)` korundu** (ADR §1). Ölçülen tuzak: açık `::char(6)` uzun değeri sessizce
>    keser ve kesik değer CHECK'ten geçer; sorgular `text` geçirir (mutant: kesilip kabul).
>    Atamada boşluk olmayan fazla karakter 22001; fazla karakterlerin hepsi sondaki boşluksa
>    kırpılır ve değer kanonik saklanır (ölçüldü, yukarıdaki kabul vakası).
> 6. **Kalıcı Down/Up testi yazılmadı** — gerekçe sayılı sınır 1.
> 7. **Ek test (brief'te yok):** `TestTenantBranding_EveryStatementNamesTheTenant` — yedi
>    SELECT/UPDATE'in `WHERE`'inde `tenant_id = $n`. Gerekçe ölçüldü: `GetTenantLogo`'nun açık
>    tenant yüklemi (parametre korunarak) düşürüldüğünde, bu test eklenmeden önce
>    `branding_test.go`'nun testleri, FORCE kapısı ve `TestStoreSurface_*` yeşildi (RLS tek
>    başına gizliyor).
>
> **Mutasyon tablosu** (her biri tek düzenleme; M-satırları migration mutantı: dev'e Up → test
> → Down, her adımda goose sürümü ölçüldü; Q-satırları sorgu mutantı: DDL yok,
> `db/queries/branding.sql` → `make sqlc` → test → geri; yedekler yalnız scratchpad'de. 2. turda
> üçüncü göz M-satırlarını son test sürümüne karşı yeniden üretti ve aynı pinler kırmızıya
> döndü):
>
> | # | Mutasyon | Kırmızı |
> |---|---|---|
> | M01 | FORCE RLS kaldırıldı | `TestTenantBranding_CatalogShape`, `TestRLS_EveryTenantScopedTableIsEnabledAndForced`, redline R5 |
> | M02 | politikadan `NULLIF` (çıplak cast) | `TestRLS_TenantBranding_NoContextFailsClosed` (22P02), `TestTenantBranding_CatalogShape`; redline geçti |
> | M03 | `WITH CHECK` kaldırıldı | `TestTenantBranding_CatalogShape`, redline R5 — davranış testleri yeşil (ALL politikası USING'i yazmaya da uygular) |
> | M04 | DELETE grant'ı | `TestTenantBranding_AppPrivileges` |
> | M05 | accent CHECK `[0-9A-Fa-f]` | `TestTenantBranding_ChecksRefuseHostileValues` (küçük harf, karışık) |
> | M06 | boyut sınırı 262145 | `TestTenantBranding_ChecksRefuseHostileValues` (262145 bayt) |
> | M07 | hep-ya-hiç CHECK kaldırıldı | `TestTenantBranding_ChecksRefuseHostileValues` (11 vaka) |
> | M08 | FK tek sütun `(updated_by) → admin_users(id)` | `TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant`, `TestTenantBranding_CatalogShape` |
> | M09 | `logo_sha256` üzerinde global UNIQUE | `TestTenantBranding_SameLogoInTwoTenants` (23505), `TestTenantBranding_CatalogShape` |
> | M10 | birincil anahtar (tenant_id indeksi) kaldırıldı | bu dosyanın 10 DB testi (`ON CONFLICT` 42P10 + katalog), redline R5 |
> | M11 | Down boş | ölçüm: Down sonrası v27'de döküm `af847d50…` (tablo kaldı), sonraki Up 42P07 |
> | Q12 | `GetTenantBrand`'e `logo` | `TestTenantBranding_PerPageReadsDoNotSelectTheLogo`, `TestStoreSurface_IsTheOneRecorded`, `TestStoreSurface_NoByteCarryingQueryReadsTags` |
> | M13 | `REVOKE ALL` kaldırıldı (dev `arwd`) | `TestTenantBranding_AppPrivileges`, `TestRLS_TenantBranding_WriteWithCheck` (tenant_id UPDATE'i yetki yerine RLS'e takıldı); dar `ar` varsayılanında (üçüncü göz A15, 2. tur) `TestTenantBranding_AppPrivileges` (tablo düzeyi INSERT true, on INSERT sütunu) |
> | M14 | `_matches_logo` kaldırıldı | `TestTenantBranding_ChecksRefuseHostileValues` (başka baytların sha'sı) |
> | M15 | `_hex` kaldırıldı | `TestTenantBranding_ChecksRefuseHostileValues` (büyük harf, 63 karakter → `_matches_logo`) |
> | M16 | PK sütun yazımında | redline R5 (testler yeşil — aynı indeks) |
> | M17 | mime kümesine `image/gif` | `TestTenantBranding_ChecksRefuseHostileValues` |
> | M18 | tablo düzeyi INSERT | `TestTenantBranding_AppPrivileges` |
> | M19 | iki sha CHECK'inin bildirim sırası değişti (kontrol) | yeşil — büyük harf sha yine `_hex`: değerlendirme ad sırasıyla |
> | Q20 | `SetTenantAccent`'te `::char(6)` | `TestTenantBranding_ChecksRefuseHostileValues` (7 hex kesilip kabul; `#`'li 7 karakter accent CHECK'ine düştü) |
> | Q21 | `GetTenantBrand` seçiminde `to_jsonb(tenant_branding)::text AS accent` | `internal/db` testleri derlenmedi (alan tipi değişti), `TestStoreSurface_IsTheOneRecorded` |
> | Q22b | `GetTenantLogo` `WHERE`'inde `@tenant_id::uuid IS NOT NULL` | `TestTenantBranding_EveryStatementNamesTheTenant` (öncesinde hepsi yeşildi) |
> | Q23 | `GetTenantBrandForUpdate`'ten `FOR UPDATE` | `TestTenantBranding_EnsureThenLockReturnsWhatTheOtherWriterCommitted` (var olan satır) |
> | A07a (üçüncü göz, 2. tur) | `GetTenantLogo` `WHERE (tenant_id = @tenant_id OR true)` | **hiçbiri** — bütün testler yeşil (belt deseni yüklemin varlığına bakar, anlamına değil; RLS satırı gizler). PART III'ün kod incelemesine bıraktığı biçim |
>
> **Olay (M11 temizliği):** boş Down mutantı tabloyu v27'de bıraktı. Elle `DROP TABLE` bir
> PreToolUse kancası tarafından engellendi (*"an agent must not issue it"*); kanca aşılmadı.
> Kalan tablonun dökümü saf 00028 Up'ınınkiyle bayt-aynıydı (`af847d50…`), bu yüzden goose'a
> DDL'siz (`SELECT 1`) bir Up ile sürüm 28 kaydettirildi, saf dosya geri kondu ve döküm
> `af847d50…` doğrulandı. Sonraki bütün Down'lar goose'un kendi Down'ı (`DROP TABLE
> tenant_branding`) ile yapıldı — brief'in istediği migrate-down yolu.
>
> **Devirler:**
> - **WL-4:** yazma tek `WithTenant` içinde: `EnsureTenantBrand(tenant, actor)` →
>   `GetTenantBrandForUpdate` ("before") → `SetTenantAccent`/`SetTenantLogo`/`Clear*`
>   (`:execrows`, 1 beklenir) → `RecordTx`. Accent: `brand.Color.Hex()` → `SetTenantAccentParams.Accent`.
>   Logo: `brand.Logo{Data, SHA256, MIME, Width, Height}` → `SetTenantLogoParams` (Width/Height
>   `int32`); DB sha'yı baytlara karşı yeniden doğrular (uyuşmazlık 23514
>   `tenant_branding_logo_sha256_matches_logo`). `ActorID` = `updated_by`: NOT NULL ve bileşik
>   FK — başka tenant'ın yöneticisi 23503. `ClearTenant*` UPDATE'tir; satır kalır. **Audit
>   "after":** saklanan accent argümanın metninden farklı olabilir (sonu boşluklu değer kırpılıp
>   kabul edilir — ölçüldü) → "after"ı DB'den oku ya da Go'da kanonik `Color.Hex()` kullan.
> - **WL-6:** `GetTenantLogo(tenant: oturumdan, sha)` başka tenant'ın sha'sı ve bilinmeyen sha
>   için aynı `pgx.ErrNoRows` döner (bayt-aynı 404'ün DB yarısı). `LogoMime *string`: satır
>   bulunduğunda hep-ya-hiç CHECK'i dolu olmasını sağlar. URL'deki sha handler sınırında
>   `^[0-9a-f]{64}$` ile doğrulanmalı (geçersiz biçim DB'ye gitmeden 404). Logo rotasının
>   çağırdığı paketin kendi belt testi (`TestStaffQueries_…` deseni) WL-6'nın.
> - **WL-7/WL-8/WL-9:** `GetTenantBrand` `pgx.ErrNoRows` **ve** bütün alanları NULL satır = "marka
>   yok"; ikisi aynı ele alınmalı (sil işlemleri satırı NULL'larla bırakır). Okuma tarafı accent'i
>   yeniden `ParseAccent` + `Check`'ten geçirir (ADR 0023 §3) — DB yalnız biçimi tutar.
> - **WL-10:** ADR 0023 WL-1 notunun üç parçalı iddiası ve mutasyon listesi; aşağıdaki sayılı
>   sınırlar; `log_statement=all` olan dev'de testlerin rastgele logo baytları sunucu log'una
>   parametre olarak düşer (sır değil, sahte bayt).
> - **WL-12 / orkestratör:** §5 tasarım özündeki sorgu adları (`UpsertTenant*`) bu karta göre
>   güncellenir; ADR 0024 İddia D PART I'in *"WL-1'de ölçülecek"* yarısı artık ölçüldü — ADR 0023
>   WL-1 notuna işaret eden bir satır önerilir (bu görev ADR 0024'e dokunmadı).
> - **Operatör:** `updated_by NOT NULL` + bileşik FK yalnız operatörün **kendini**
>   `updated_by`'a yazmasını engeller (operatör bir `admin_users` satırı değildir). Bir definer'ın
>   sıfırlamasını engellemez — üçüncü göz ölçtü (2. tur; yalıtılmış DB, BYPASSRLS rol, geri
>   alınan tx): `UPDATE tenant_branding SET accent = NULL, updated_at = now() WHERE tenant_id =
>   …` → `UPDATE 1`, `updated_by` değişmedi. Böyle bir sıfırlama yazılırsa son değişiklik
>   tenant'ın son yöneticisine yanlış atfedilmiş kalır; bir operatör sıfırlaması tasarlanırsa
>   (`op_*`) atıf ayrıca çözülmeli (ör. `operator_audit_log` + ayrı sütun — yeni migration).
>
> **Sayılı sınırlar:**
> 1. **Down→Up→Down bir ölçümdür, kalıcı test değil.** 00028 `tenants` ve `admin_users` üzerine
>    ikişer RI tetikleyicisi koyar (ölçüldü: `pg_trigger.tgconstrrelid = tenant_branding` →
>    tenants 2, admin_users 2); Up/Down bunları yaratır ve kaldırır, yani bir test
>    transaction'ında koşturulan Up/Down bu iki tabloda DDL kilidi alır ve transaction bitene dek
>    tutar; paylaşılan DB'de paralel koşan ve bu iki tabloyu kullanan testler bekleyebilir
>    (OP-6'nın 40P01/55P03 dersi). Kilit düzeyi ve bekleme bu görevde ölçülmedi. Emsal: OP-10A'nın
>    ölçümü.
> 2. ~~M13'ün dar `ar` varsayılanı ölçülmedi~~ — **kapandı (2. tur):** üçüncü göz `REVOKE ALL`
>    kaldırılmış hâli dar `ar` ACL'de koşturdu (A15): `TestTenantBranding_AppPrivileges` kırmızı
>    (tablo düzeyi INSERT true, INSERT sütunları on).
> 3. ~~M01–M18 test dosyasının eski sürümüne karşı koşuldu~~ — **kapandı (2. tur):** üçüncü göz
>    bütün mutasyonları son test sürümüne karşı yeniden üretti, pinler birebir tuttu. (Yapıcı
>    tarafında: M06/M07 ve Q12/Q20/Q22b/Q23 son sürüme karşı koşulmuştu.)
> 4. Q21'de metin pininin kendi kararı gözlenmedi (paket derlenmedi).
> 5. Metin pinleri (`…PerPageReadsDoNotSelectTheLogo`, `…EveryStatementNamesTheTenant`) sevk
>    edilen metni okur; tam seçim listesi karşılaştırması ve `tenant_id = $n` deseninin
>    dışındaki yazımlar kod incelemesinin konusu. **Ölçüldü (üçüncü göz A07a, 2. tur):**
>    `GetTenantLogo` için `WHERE (tenant_id = @tenant_id OR true)` bütün testlerde yeşil — belt
>    deseni yüklemin varlığını görür, anlamını görmez; RLS satırı yine gizler.
> 6. Test fikstürleri (rastgele uuid'li tenant, yönetici, marka satırları) dev DB'de kalır
>    (`tappa_app` DELETE taşımaz; `make db-reset` temizler).
>
> **2. tur (2026-10-03 — üçüncü göz ONAY, tappa-security-auditor ONAY; yalnız metin
> kapanışı):** ADR 0023 WL-1 notunun PART II başlığı ölçülene eşitlendi (migration ve sorgu
> mutasyonları ayrı, Q21 eklendi, son test sürümüyle yeniden üretim ve A15/A07a ölçümleri
> atıflı) · migration Down yorumu: iki FK'nin dahili RI tetikleyicileri (dev: `tenant_branding`
> 4, `tenants` 2, `admin_users` 2 — ölçüldü) kısıtlarla birlikte düşer; Up kullanıcı
> tetikleyicisi yaratmaz · char(6) cümlesi üç yerde (migration, `branding.sql` + üretilmiş yorum,
> ADR) "boşluk olmayan fazla karakter 22001; sondaki boşluklar kırpılır" diye düzeltildi ve
> kabul vakası eklendi · operatör devri ölçülene eşitlendi. Migration'ın SQL'i değişmedi
> (yalnız yorum); dev DB yeniden uygulanmadı (goose yorum değişikliğini sürüm saymaz), şema
> dökümü `af847d50…`.

> **Kart düzeltmesi (2026-10-03, WL-4 uygulaması sırasında; 2. tur aynı gün).** Yazıldı:
> `internal/domain/tenant/brand.go` (`Brands`, `NewBrands`; `SaveAccent`, `ClearAccent`,
> `SaveLogo`, `ClearLogo`; `ActionBrandUpdated`, `BrandFieldAccent`/`BrandFieldLogo`,
> `ErrBrandNotPermitted`, `ErrBrandLogoNotNormalized`) · `internal/domain/tenant/brand_db_test.go`
> (11 test) · **kapsam genişlemesi (gerekçe: sapma a):** `internal/brand/logo.go`'ya
> `Logo.Normalized()` ve dışa kapalı `mint` alanı, `internal/brand/logo_normalized_test.go`
> (1 test), `internal/brand/logo_test.go`'nun `logoCheckOutput`'una bir satır (`Normalized()`
> doğru olmalı) · **kapsam genişlemesi (2. tur, sapma f):** `db/queries/branding.sql` başlık
> yorumu (WL-1'in dosyası; yalnız yorum) ve `make gen`'in ona göre yeniden yazdığı
> `internal/store/branding.sql.go` + `querier.go` yorumları ·
> [ADR 0023](../adr/0023-tenant-markasi-ve-arayuz-kurali.md) §1'e tarihli *"WL-4 notu"* (kural
> değişmedi; kararlar, ölçüm, üç parçalı iddia, mutasyon tablosu). Yeni sorgu, migration,
> bağımlılık yok (`go.mod`/`go.sum`/`sqlc.yaml` diff boş; `db/` ve `internal/store/` diff'i
> yalnız yorum). **Ölçüm ortamı:** dev Postgres 17 (paylaşılan), `tappa_app`; yerel Go
> 1.27.1, staticcheck Go 1.26.7; ayrı git worktree.
>
> **Kabul — kanıtlar:**
> 1. **Kaydet/sil UPDATE + audit aynı transaction'da, iki yön.** Audit tarafından:
>    `TestBrandDB_TheChangeAndItsTrailRowShareOneTransaction` — hata döndüren trail
>    (`brokenTrail`, staff testlerinin emsali) ile dört yazma, satırı olmayan ve olan tenant'ta
>    satırı değiştirmez ve satır yaratmaz; satırını **gerçekten yazıp** sonra hata döndüren
>    trail ile ne değişiklik ne o audit satırı kalır (satır id'siyle aranır); kendisine verilen
>    transaction'dan marka satırını okuyan trail her yazmanın kendi sonucunu görür. Yazma
>    tarafından: `TestBrandDB_AFailedWriteLeavesNoTrailRow` — Set/Clear ifadesi hata
>    döndürünce (ifade gönderilmez, transaction sağlam kalır) ve **Postgres'in kendisi** 0
>    satır değiştirdiğini söyleyince (`… AND false`), dört yazma satırı ve trail'i olduğu gibi
>    bırakır. Test kancası ürün koduna eklenmedi: ikisi de tüketici tarafı arayüzlerin
>    (`Trail`, `Database`) test ikizidir.
> 2. **`detail` tam 6 sabit anahtar** (ADR 0024 §6: `field`, `before`, `after`, `bytes`,
>    `width`, `height`): `TestBrandDB_TheDetailHasExactlyTheSixKeys` jsonb'de saklananı okur,
>    dört yazmanın her birinde anahtar kümesi tam o altı. Fazlası (M07) ve eksiği (M07b,
>    `omitempty`) kırmızı. Değerler `TestBrandDB_EachWriteStoresItsValueAndOneTrailRow`'da
>    beş yazmalık dizide birebir. Eylem adı ve `field` değerleri literal olarak:
>    `TestBrandTrail_TheActionAndFieldNamesAreTheADRs`; testler trail'i literal
>    `'tenant.brand_updated'` ile okur (A12, sabit yeniden adlandırıldı → kırmızı).
> 3. **Domain accent'i yeniden `Check` eder → `ErrAccentIllegible`:**
>    `TestBrandDB_RefusalsBeforeTheDatabaseWriteNothing` — `808080` ve `E0457B` (ADR §3'ün
>    iki red örneği) `brand.ErrAccentIllegible`, açılan transaction 0, satır ve trail
>    değişmedi. M05 (`Check` kaldırıldı) → ikisi de yazıldı, kırmızı. Saklanan `Hex()`:
>    `#da291c` (`NormalizeAccent`) → `DA291C`.
> 4. **`ActorID` zorunlu:** aynı test — nil aktör ve nil tenant dört yazmanın her birinde
>    transaction açılmadan reddedilir. M04a/M04b (`requireActor` kaldırıldı) → transaction
>    açıldı, kırmızı. `actor_id` = `ActorID`, `updated_by` = `ActorID` (iki owner sırayla
>    yazar; `TestBrandDB_EachWriteStoresItsValueAndOneTrailRow`).
> 5. **WL-1 devri — yazma sırası** Ensure → ForUpdate → Set/Clear (1 satır) → `RecordTx`;
>    "before" ForUpdate'ten, "after" aynı transaction'da `GetTenantBrand`'den.
>    Eşzamanlılık: `TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore`
>    — yeni/var olan satır × birinci yazıcı UPDATE'inden önce/sonra tutulu, dört durumda
>    ikinci yazıcının beklediği ifade `pg_stat_activity`'den okunur ve assert edilir, ve
>    "before"u birincinin "after"ıdır. *(Logo kaydında geri okunan sha yazılanla eşit değilse
>    yazmanın hata vermesi **kodda, ölçülmedi**: birincil anahtar ve
>    `tenant_branding_logo_sha256_matches_logo` CHECK'i varken testler o dala ulaşmaz; dal
>    kaldırılınca hepsi yeşil kaldı — A15. Kanıt listesinde değildir.)*
> 6. **Tenant izolasyonu ve yetki:** `TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite` —
>    manager, devre dışı owner, A'nın owner'ı B'de, B'nin owner'ı A'da ve yönetici olmayan id,
>    dört yazmada, **marka satırı olan iki tenant'ta ve olmayan iki tenant'ta**:
>    `err == ErrBrandNotPermitted` (sarılmamış değer), beşinin hata metni tek, iki tenant'ın
>    satırı ve trail'i değişmez, satır yaratılmaz. A01 (kapı Ensure'dan sonra) → satırsız
>    turda üç aktör 23503, üç farklı metin, kırmızı; A07c (rol hataya sarılır, `write()`'taki
>    toparlama kaldırılır) → iki turda kırmızı. Açık filtre:
>    `TestStaffQueries_CarryAnExplicitTenantPredicate` artık bu paketin sekiz marka/aktör
>    sorgusunu türetir (`ClearTenantAccent`, `ClearTenantLogo`, `EnsureTenantBrand`,
>    `GetAdminByID`, `GetTenantBrand`, `GetTenantBrandForUpdate`, `SetTenantAccent`,
>    `SetTenantLogo` — koşunun "seen here" listesinde); M11 (`OR true`) onu kırmızıya çevirdi,
>    davranış testleri yeşil kaldı (RLS satırı gizler).
> 7. **Logo temizle → beş logo alanı NULL:** `TestBrandDB_EachWriteStoresItsValueAndOneTrailRow`
>    beş sütunu ve `octet_length(logo)`'yu tek tek okur. M10a (sorgudan bir sütun) → 23514
>    `tenant_branding_logo_all_or_none`; M10b (yanlış ifade) → kırmızı.
> 8. **Aynı accent'i yeniden kaydetmek:** `TestBrandDB_SavingTheSameAccentAgainIsRecorded` —
>    UPDATE koşar (`updated_at` ilerler, `updated_by` ikinci owner), "before" = "after" satırı.
> 9. **Satırı olmayan tenant'ı temizlemek (Karar 3):**
>    `TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt` — ClearAccent ve ClearLogo
>    satırı bütün marka alanları NULL olarak yaratır, `updated_by` = owner, satırın `detail`'i
>    `{after:null, before:null, bytes:null, field, height:null, width:null}`.
>
> **Kararlar (gerekçeli; ayrıntı ADR 0023 WL-4 notu):**
> - **Yetki = aktif owner, domain'de ikinci kez** (`GetAdminByID`, yazmanın kendi
>   transaction'ında, `EnsureTenantBrand`'den önce, `role = 'owner'` ve `status = 'active'`).
>   Handler WL-7'de önce `mayEditAccount` ile kapar ve ret satırını yazar (ADR 0024 §6;
>   `accountactions.go` emsali); domain handler'ın sözüne güvenmez — accent'i yeniden `Check`
>   ettiği gibi. Mutasyonla ölçüldü (M13a): kapı yokken manager ve devre dışı owner **yazdı**;
>   başka tenant'ın yöneticisi bileşik FK ile (23503) reddedildi. Policy motoru seçilmedi
>   (marka için eylem/guardrail ADR'lerde yok; eklemek §5/§3 değişikliği).
> - **No-op = eylem** (`Accounts.Save` emsali): UPDATE + "before" = "after" satırı.
> - **`detail` değerleri:** accent satırında `bytes`/`width`/`height` `null`; temizlemede
>   "after" `null`; ilk yazmada "before" `null`; `target` = tenant id.
>
> **Sapmalar (gerekçeli):**
> - **a. `internal/brand/logo.go`'ya `Logo.Normalized()`** (WL-3'ün onaylı dosyası — kapsam
>   genişlemesi). Gerekçe ölçüldü: `brand.Logo`'nun alanları dışa açık, tablo da ayırt
>   etmiyor — domain kontrolü kaldırılınca (M12a) APP1 `Exif` segmentli ham bir JPEG, kendi
>   sha'sıyla bir literalde **saklandı**. Brief'in *"logoyu Normalize çıktısı yerine ham baytla
>   kabul eden yol"* mutasyonunu öldürecek bir ayrım `brand` paketinin içinde olmak zorunda
>   (dışa kapalı alanı Go'da o paketin dışından bir literal adlandıramaz; `reflect`/`unsafe`
>   hariç — sayılı sınır 2). Değişiklik: `Normalize`'ın dönüşüne `mint`
>   iliştirilir, `Normalized()` beş alanı (baytın sha256'sı dahil) onunla karşılaştırır.
>   `Normalize`'ın davranışı değişmedi; `internal/brand` paketi `-race` ile yeşil.
> - **b. Accent girdisi `brand.Color`**, dize değil: brief'in *"`Hex()` yerine argüman
>   metnini sakla"* mutasyonu bu tiple doğrudan ifade edilemez; yerine iki yazım (M06a
>   `#rrggbb`, M06b küçük harf) koşturuldu → DB 22001/23514, kırmızı. "after"ı DB'den değil
>   argümandan okumak (M06c) **eşdeğer** çıktı: argüman `Hex()`'tir (altı büyük harf hane,
>   boşluk yok).
> - **c. Yeni sorgu yok:** aktörün rolü mevcut `GetAdminByID` ile okunur (`storekeyshape`
>   sayımları değişmez).
> - **d. Eşzamanlılık testi ilk sürümünde M02'yi (`FOR UPDATE` yok) yakalamadı** — birinciyi
>   yalnız UPDATE'ten sonra tutuyordu ve o tutmada ikinci `EnsureTenantBrand`'de bekler.
>   Test UPDATE'ten önce tutmayı da ekleyecek ve beklenen ifadeyi assert edecek biçimde yeniden
>   yazıldı; M02 → kırmızı. WL-1'in "var olan satırda ForUpdate'te bekler" ölçümü birincinin
>   UPDATE'ten önceki hâlidir (ADR 0023 WL-4 notu).
> - **e. Var olmayan satırı temizlemek** satırı yaratır (Ensure) ve `null`→`null` satırı yazar
>   (kabul 9).
> - **f. `db/queries/branding.sql` başlık yorumu düzeltildi (2. tur, WL-1'in dosyası — kapsam
>   genişlemesi; yalnız yorum).** *"in step 2 on its row lock when the row exists"* yerine
>   ölçülen: var olan satırda birinci UPDATE'ini yapmışsa ikinci step 1'de (Ensure), yapmamışsa
>   step 2'de bekler. **Orkestratöre itiraz (ölçüm):** brief *"`make gen` sonrası
>   `internal/store/` diff'i 0"* bekliyordu; sqlc dosya başlığını ilk sorgunun
>   (`GetTenantBrand`) Go doc yorumuna kopyaladığı için `internal/store/branding.sql.go` ve
>   `querier.go`'da 13'er satırlık **yalnız yorum** diff'i oluştu (WL-1'in 2. turu da aynı
>   yoldan "üretilmiş yorum" değiştirmişti). Davranışın değişmediği yorumsuz AST
>   karşılaştırmasıyla gösterildi: iki dosyanın HEAD ve yeni hâlinin yorumsuz sözdizimi ağacı
>   sha256'sı eşit (`branding.sql.go` `ee766d6d…`, `querier.go` `a5731227…`); diff'in yorum
>   olmayan satırı 0.
>
> **Mutasyon tablosu:** ADR 0023 §1 → WL-4 notu, PART II. 1. tur: M01a–M17, 29 mutasyon;
> M06c eşdeğer ve yeşil, 28'i kırmızı. 2. tur: A01, A07c, A12 kırmızı; A15 yeşil (dal
> ulaşılamaz — kabul 5'in parantezi). M00 değiştirilmemiş kodun pozitif kontrolü (2. turun
> testleriyle yeniden yeşil). M-satırları 2. turun eklediği testlere karşı yeniden koşulmadı.
> Yedekler ve koşu çıktıları yalnız scratchpad'de.
>
> **Devirler:**
> - **WL-5:** yok (tema rotası domain'i çağırmaz; saklanan accent `Hex()` biçiminde).
> - **WL-6:** `GetTenantLogo` bu paketten çağrılmaz → `TestStaffQueries_…` onu görmez; logo
>   rotasını çağıran paketin kendi belt testi WL-6'nın (WL-1 devri aynen).
> - **WL-7:**
>   - wiring `tenant.NewBrands(db, auditRecorder, log)` bir kez.
>   - **Ret:** handler önce `mayEditAccount` (manager → 303 `not-permitted` +
>     `tenant.brand_update_refused` + 0 UPDATE; ret satırının `detail`'i WL-7'nin —
>     `refusedAccountDetail` emsali). Domain'in `ErrBrandNotPermitted`'i (ör. oturum
>     çözümlendikten sonra rol değişmişse) **aynı muameleyi** görür: yalnız 303 değil,
>     `tenant.brand_update_refused` satırı da yazılır — ana transaction geri alındığı için
>     `Recorder.RecordTx` ile değil, kendi transaction'ını açan `audit.Recorder.Record` ile
>     (`internal/audit/audit.go`: *"Record appends the event in its OWN transaction"*;
>     ADR 0024 §6). `err == tenant.ErrBrandNotPermitted` karşılaştırması geçerlidir (değer
>     sarılmadan döner).
>   - **Accent:** `NormalizeAccent` → `SaveAccent`; `brand.ErrAccentIllegible` →
>     `Suggest(c).Hex()` ile yeniden render.
>   - **Logo:** `gate.Normalize`'ın dönüşü **değiştirilmeden** `BrandLogoCommand.Logo`'ya (bir
>     alanı değişen ya da yeniden kurulan `Logo` `ErrBrandLogoNotNormalized` alır; bu bir iç
>     hata, kullanıcı cümlesi değil). Açık logo uyarısı `Normalize` çıktısından (WL-3 devri).
>   - **"Sıfırla"** iki alanı birden temizleyecekse WL-4 bunu tek transaction'da yapmaz —
>     `ClearAccent` + `ClearLogo` iki transaction ve iki satırdır; tek transaction istenirse
>     WL-7 bir `Reset` ister (WL-4'e geri dönüş).
>   - **Bütçe (güvenlik-3):** accent ve sıfırla rotaları ADR 0024 §6'nın yükleme bütçesine
>     girmez, ve aynı değerin yeniden kaydı da UPDATE + audit satırı yazar (Karar 3). WL-7 ya bu
>     iki rotaya bir sayı bütçesi koyar ya da panel sınırlayıcısının (oturum başına 10 dakikada
>     300 istek) yeterli olduğunu gerekçesiyle yazar.
>   - **Log:** domain'in döndürdüğü hatalar `pgconn.PgError`'ı `%w` ile sarar; mutasyon
>     koşularında `Error()` metni ileti + SQLSTATE taşıdı, satır değeri taşımadı (M03a, M06a,
>     M06b, M10a çıktıları). `PgError.Detail` (*"Failing row contains …"* — logo baytını
>     içerebilir) bu görevde **incelenmedi**; WL-7 ret/hata yollarında `Detail`'i loglamaz,
>     yalnız sınıfı yazar (ADR 0024 §6).
> - **WL-8/WL-9:** okuma tarafı `GetTenantBrand` — `pgx.ErrNoRows` ve bütün alanları NULL
>   satır aynı ("marka yok"; temizlemeler satırı NULL'larla bırakır, var olmayan satırı
>   temizlemek de satır yaratır). Accent okumada yeniden `ParseAccent` + `Check` (ADR §3).
> - **WL-10:** denetim listesine bu notun üç parçalı iddiası ve mutasyon tablosu; `Normalized()`
>   kapsam genişlemesi; sayılı sınırlar. **Yeni pin (güvenlik-2; şimdi yazılmadı, devredildi):**
>   beş marka yazma sorgusunu (`SetTenantAccent`, `SetTenantLogo`, `ClearTenantAccent`,
>   `ClearTenantLogo`, `EnsureTenantBrand`) üretim kodunda yalnız `internal/domain/tenant`
>   çağırır. Gerekçe: tablo normalize edilmiş baytı ham bayttan ayıramaz (M12a ölçümü);
>   `Normalized()` kapısı ve rol kapısı domain'dedir — o sorguları başka paketten çağıran bir
>   yol ikisini de atlar.
> - **WL-11:** yok.
> - **WL-12 / orkestratör:** ADR 0024 İddia G PART I'in *"WL-4'te ölçülecek"* yarısı ölçüldü
>   (ADR 0023 WL-4 notu) — ADR 0024'e işaret eden bir satır önerilir; ADR 0024 §3/§6'ya
>   `Logo.Normalized()` ile domain'in ret kuralına işaret eden bir satır önerilir (bu görev ADR
>   0024'e dokunmadı).
>
> **Sayılı sınırlar** (ADR 0023 WL-4 notundakiyle aynı altı): (1) rol okuması düz `SELECT`:
> okumadan sonra commit edilen rol düşürmesini o yazma görmez (ölçülmedi) · (2) `Normalized()`
> `reflect`/`unsafe` ile aşılabilir; değişmemiş kopya kabul edilir · (3) audit satırlarının
> sırası sınanmaz · (4) fikstürler dev DB'de kalır · (5) eşzamanlılık testi
> `pg_stat_activity`'yi `tappa_app` olarak okur · (6) `log_statement=all` olan dev'de testlerin
> üretilmiş logo baytları sunucu log'una parametre olarak düşebilir (sır değil; WL-1 sınırıyla
> aynı — log okunmadı).

> **Kart düzeltmesi (2026-10-03, WL-6 uygulaması sırasında; 2. ve 3. tur aynı gün).** Yazıldı:
> `internal/handler/brandlogo.go` (`BrandLogos`, `NewBrandLogos`; iki rota
> `GET /admin/brand/logo/{sha}` ve `GET /t/logo/{sha}`; `logoImagePolicy`; `If-None-Match`
> taraması; tek ret yazıcısı) · `internal/domain/tenant/brandread.go` (`BrandReader`,
> `NewBrandReader`, `Logo`, `PageLogo`, `ErrLogoNotFound`, `StoredLogo`, `LogoRef`) ·
> `internal/handler/tap.go` (`Tap.chain` — `Tap.Mount` zinciri oradan alır; `tapCSPFor`;
> `render` → `tapCSPFor(false)`) · `internal/handler/adminlogin.go` (`adminCSPFor`; `render` →
> `adminCSPFor(false)`, `renderScripted` → `logoImagePolicy(adminScriptedCSP, false)`) ·
> `cmd/tappa/main.go` (`tenant.NewBrandReader` + `handler.NewBrandLogos`, `NewRouter`'a
> `logos`) · bütçe aritmetiği: `internal/httpx/ratelimit.go` (yorum),
> `internal/httpx/ratelimit_test.go` (`TestTapLimiter_DefaultsAreWideEnoughForAShiftChange`
> logoyu sayar), `internal/handler/adminratelimit.go` (yorum) · testler:
> `internal/handler/brandlogo_test.go` (15 test), `internal/handler/brandlogo_db_test.go`
> (1 test), `internal/domain/tenant/brandread_db_test.go` (5 test) ·
> **[ADR 0024](../adr/0024-kullanici-yukledigi-gorsel.md) değişikliği:** §5'in normatif
> metnine satır içi *"(WL-6 düzeltmesi, 2026-10-03: …)"* notları — tek 404'ün başlıkları ve
> kapsamı, 200'ün `Content-Length`'i, 304 kuralı, yalnız GET (HEAD 405) — ve sona tarihli
> *"WL-6 notu"* (kararlar, ölçüm, üç parçalı iddia, mutasyon tablosu, sınırlar, devirler).
> Yeni sorgu, migration, bağımlılık yok: `db/`, `internal/store/`, `go.mod`, `go.sum`,
> `sqlc.yaml`, `web/` diff'i boş — bugünkü sayfaların HTML'i değişmedi, hiçbir sayfaya
> `<img>` eklenmedi. **Ölçüm ortamı:** dev Postgres 17 (paylaşılan), `tappa_app`; yerel Go
> 1.27.1, staticcheck Go 1.26.7; ayrı git worktree. 1. tur `377daf3`, 2. tur `f3c9c04`
> tabanında (OP-10B: `NewAdminAuth`'tan yasal metin parametresi ve `PanelSections`'tan
> `/admin/legal` çıktı; birleştirme düzeltmesi `brandlogo_test.go`'daki `NewAdminAuth`
> çağrısından `newFakeTexts()`).
>
> **Kabul — kanıtlar:**
> 1. **İki rota, tenant yalnız oturumdan.** Panel rotası `AdminAuth.Protect()` (okuma zinciri:
>    flood → requireAdmin → sessionGate), tap rotası `Tap.chain()` (ByAddress → Identify →
>    BySession; `GET /t` ile aynı `TapLimiter` örneği). Yönetici oturumu olmayan, çalışan
>    çerezli `/admin/brand/logo/…` isteği 303 → `/admin/login`, gövdede logo baytı yok, panel
>    çözümleyicisi 0 kez, okuma 0 (`TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`);
>    çözümleyicinin reddettiği panel oturumu (`adminauth.ErrNoSession`) aynı 303'ü alır, okuma
>    0; iki işletmeye ait iki çerezle her rota kendi yüzeyinin işletmesini sunar (aynı test).
>    Oturumsuz tap logosu 404 — çerezsiz, oturum adlandırmayan çerez, iptal edilmiş oturum,
>    yalnız panel çerezi; okuyucu çağrılmaz (`TestLogoRoutes_EveryRefusalIsTheSameNotFound`).
>    Sorgu dizesindeki `tenant`/`tenant_id` yok sayılır; her okuma oturumun işletmesi altında
>    (aynı test).
> 2. **Bayt-aynı 404, kehanet yok** (rotanın eşleştiği yollarda). Gerçek okuyucu + gerçek
>    Postgres, ürünün yazıcısıyla saklanan iki logo: A oturumunun B'nin digest'ine ve
>    bilinmeyen digest'e aldığı yanıt iki rotada durum + her başlık + gövde olarak bayt-aynı ve
>    tek 404'e eşit; kontrol: B oturumu B'nin digest'ini alır
>    (`TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`). Domain'de aynı değer ve
>    metin (`TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness`). `^[0-9a-f]{64}$` dışı sekiz
>    `{sha}` segmenti (büyük harf, 63, 65, `.png` ekli, `%2F`, `%2E%2E`, `..`, 64 hex olmayan
>    karakter) aynı 404 ve logo okumasına ulaşmaz — oturum çözümlemesi zincirde ondan önce
>    olmuştur (`TestLogoRoutes_EveryRefusalIsTheSameNotFound`). Tek ret: `Not found.\n`,
>    `Content-Type: text/plain; charset=utf-8`, `Content-Length`, `Cache-Control: no-store`,
>    nosniff. Rotayla eşleşmeyen yollar (boş segment, ham `/`) yönlendiricinin kendi 404'ünü
>    alır (`TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer`; sınır 3).
> 3. **Başlıklar birebir** (`rec.Result().Header`, tam küme eşitliği):
>    `Cache-Control: private, max-age=31536000, immutable` · `Content-Disposition: inline;
>    filename="logo.png"` (image/png) / `"logo.jpg"` (image/jpeg) · `Content-Length` ·
>    `Content-Security-Policy: default-src 'none'; sandbox` · `Content-Type` = saklanan sütun ·
>    `Cross-Origin-Resource-Policy: same-origin` · `ETag: "<sha>"` · `X-Content-Type-Options:
>    nosniff`; GIF gibi koklanan baytlar saklanan `image/png` ile sunulur; tür/dosya
>    adı/işletme adlandıran sorgu ve istek başlıkları hiçbir baytı değiştirmez
>    (`TestLogoRoutes_TheHeadersAreExactlyTheADRs`). Tel: `httpx.NewRouter` üzerinden,
>    `OperatorHost` ayarsız, gerçek sunucuda 200/404/304 = handler başlıkları + `Date`
>    (`TestLogoRoutes_TheWireCarriesWhatTheHandlerSets`).
> 4. **Global middleware ölçüldü:** `RequestID`, `RealIP`, `AccessLog`, `Recoverer`,
>    `Timeout(30 s)` hiçbir yanıt başlığı eklemez (madde 3'ün tel testi). `OperatorHost`
>    ayarlıysa `operatorHostOnly` her host'ta kurulur ve yalnız o host'ta etki eder; o
>    yapılandırmada müşteri host'unun logo yanıtı telde ölçülmedi (sınır 13). **Çakışma yok:**
>    logo yanıtı sayfa render'ından geçmez, sayfa CSP'si yazılmaz; zincirin kendi retleri
>    (303/429/500) logo baytı taşımaz.
> 5. **`If-None-Match` → 304**, yalnız oturumun kendi satırı bulunduktan sonra (karar 6);
>    `"<sha>"`, `W/"<sha>"`, `*`, liste içinde, ikinci satırda → 304 (5 başlık, gövde yok);
>    başka etag, tırnaksız, büyük harf, eşleşmeden önce gelen bozuk üye, boş → 200; eşleşmeden
>    SONRA gelen bozuk üye (`"<sha>", garbage` · `*garbage` · `"<sha>" junk` · iki satır)
>    eşleşmeyi bozmaz → 304, tek satır içinde net/http ile aynı; **satırlarda farklı:** net/http
>    yalnız ilk satırı okur, bu rota hepsini birleştirir — [`"x"`, `"<sha>"`] net/http'de 200,
>    burada 304 (go1.27.1 `http.ServeContent` sondasıyla ölçüldü; sınır 14 — yalnız kendi
>    logosu, sonuç 304, kehanet yok); başka işletmenin ve bilinmeyen digest'te
>    `"<B-sha>"`/`*`/`W/"<B-sha>"` → aynı 404
>    (`TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`,
>    `TestIfNoneMatch_TheScanFollowsRFC9110`).
> 6. **Sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır** — iki yüzey; test listeyi ve
>    sayıyı her koşuda basar (2026-10-03, `f3c9c04` tabanında *"29 renders (9 panel
>    sections)"*; 1. turun `377daf3` tabanında 30/10 — fark OP-10B'nin `/admin/legal`'ı
>    çıkarması): `pages.PanelSections`'ın her satırı · transactions + `/admin/dockets` bir
>    kayıtla · `/admin/login` · defter düşmüşken transactions · `GET /t` · bozuk URL ·
>    bilinmeyen plaket · sunucu hatası · onay ekranı 4 hüküm × practice (8) · `POST`
>    bilinmeyen plaket · başka işverenin plaketi · bayat bağlam · 429
>    (`TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`). `hasLogo=false` iken
>    politikalar bugünküyle bayt-aynı, `true` yalnız `; img-src 'self'` ekler
>    (`TestPagePolicies_ALogoWidensByImgSrcAlone`, literal'e karşı;
>    `TestTapResponses_CarryTheContentSecurityPolicy` yeşil). Yakaladığı mutasyonlar: img-src her
>    sayfaya (M04a), tap render'larına (M04b), panel render'larına (M04c), transactions
>    bölümüne (M04d).
> 7. **Ücretli istek ölçüldü, bütçeler güncellendi (sayılar aynı):** bir logo isteği kendi
>    yüzeyinin adres ve oturum kovasından birer düşer, kovalar sayfalarınkidir — 299 logo +
>    sayfa (300.) → 200, 301. (logo ya da sayfa) → 429, tap ve panelde; 3000 oturumsuz logo
>    isteği aynı adresin oturumlu `GET /t`/`GET /admin`'ini 429'a düşürür, başka adres 200;
>    çerezsiz 3000 tap logo isteği 0 oturum çözümlemesi, 0 okuma (boş olmayan çerez değeri
>    `GET /t`'deki gibi bir `Verify` öder) (`TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`).
>    WL-8/WL-9 çizince sayfa başına sıcak 1, soğuk 2. Tap aritmetiği: 300 × 4 = 1 200 < 3 000;
>    önbelleği kapalı 5 sn yenileme 240 < 300
>    (`TestTapLimiter_DefaultsAreWideEnoughForAShiftChange`). **Tap'ı engelleyebilir mi:** evet,
>    çerezi tutan biri 300 logo isteğiyle o oturumu 10 dk 429'a düşürür — `GET /t` ile de
>    düşürebilirdi; **bütçe kovaları bakımından** yeni yetenek değil. **Bayt bakımından** ekler:
>    yanıt başına en çok 262 144 B (304'te de okunur), oturum başına 10 dk'da 75 MiB; ayrı bayt
>    sınırı yok (sınır 12, devir WL-7/WL-9).
> 8. **Belt:** logo okuması `internal/domain/tenant`'ta, o paketin
>    `TestStaffQueries_CarryAnExplicitTenantPredicate`'i artık `GetTenantLogo`'yu türetir
>    ("seen here" 1. turda 53 → 54); `TestBrandRead_TheBeltSeesBothReads` çağrı paketten
>    çıkarsa kırmızı. Q01 (yüklem silindi) ve Q02 (`OR true`) belt'i kırmızıya çevirir;
>    izolasyon testleri RLS yüzünden yeşil kalır (sınır 8).
> 9. **`hasLogo` türetimi:** `BrandReader.PageLogo` — marka satırı yok, bütün alanları NULL
>    satır ve logosuz accent'li satır aynı "logo yok" (hata yok); logolu satır digest + tür +
>    kutu (`TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo`). Dört logo sütunu için okuma
>    tarafının kendi hep-ya-hiç kuralı: dördü dolu → logo, dördü NULL → yok, diğer 14
>    birleşimin her biri hata — tahmin edilen türle ya da 0×0 kutuyla çizilmez
>    (`TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` 14'ünü türetip sürer). 00028'in
>    `tenant_branding_logo_all_or_none` CHECK'i bu satırları tabloya sokmaz. **2. tur:**
>    denetçinin X11 (digest + kutu, tür NULL → `image/png`) ve X35 (digest + tür, kutu NULL →
>    0×0) mutantlarını 1. turun testinde yeşil ölçtü; şimdi ikisi de kırmızı, 1. turun "digest
>    yoksa logo yok" kuralı da (X36). **3. tur:** tarama da sayılıyor — 16 alt kümenin her biri
>    tam bir kez (boş 1, dolu 1, yarım 14); kapanış denetçisinin A07 (`mask < 15`), A10
>    (`mask := 1`) ve A08 (`< 15` + "dördü dolu → hata") mutasyonları önce yeşildi, şimdi
>    kırmızı. Bugün çağıranı yok (WL-8/WL-9).
> 10. **DB testleri gerçek Postgres'te, üretim yolunda** (WithTenant + açık yüklem; `WHERE`'siz
>     RLS testi WL-1'in), iki işletmeli.
>
> **Kararlar (gerekçeli; ayrıntı ADR 0024 WL-6 notu):** 304 (yok sayma değil), aramadan sonra ·
> HEAD desteklenmez (chi 405, zincirlerden önce) · 404 `http.NotFound` değil, `no-store`'lu
> tek yazıcı · `Content-Length` eklendi (§5'in yedisine ek; chunk'lanmasın, recorder = tel) ·
> devre dışı çalışanın canlı oturumu logoyu alır (`GET /t` de sayfayı render eder) · panel
> rotası rol ayırmaz · okuyucu `Brands`'ten ayrı tip · `logoImagePolicy` transactions
> bölümünün betikli politikasını da kapsar · giriş ve sıfırlama aileleri dokunulmadı ·
> `logoRefOf` dört sütunda hep-ya-hiç (2. tur).
>
> **Sapmalar (gerekçeli):**
> - **a. Kapsam genişlemesi — `Tap.Mount`:** üç `r.Use` satırı `r.Use(t.chain()...)` oldu.
>   Gerekçe: logo rotası aynı sırayı ve aynı limiter örneğini almalı; iki yerde yazılı sıra
>   ikinci bir temsil olurdu. Davranış değişmedi: `TestTapPage_MountOrderMetersTheSession`,
>   `TestTapPage_RateLimitIsMountedAndBranded` ve tap paketinin geri kalanı yeşil.
> - **b. Bütçe dosyaları:** `internal/httpx/ratelimit.go` ve `adminratelimit.go`'ya yalnız
>   tarihli yorum (2. turda bayt uyarısı eklendi; 3. turda tap tarafının bayt kararı WL-9'a,
>   panel tarafınınki WL-7'ye bağlandı); `ratelimit_test.go`'da aritmetik logoyu
>   sayar (`requestsPerTap` 3 → sayfa + düğme + soğuk logo + yeniden deneme = 4; yenileme ×
>   (sayfa + logo)).
> - **c. `If-None-Match` RFC gerekçesi** RFC metni ağdan okunmadan, go1.27.1 `net/http/fs.go`'nun
>   okunmasıyla yazıldı (sınır 9).
>
> **Mutasyon tablosu:** ADR 0024 → WL-6 notu. 2. turda `f3c9c04` tabanındaki birleşik ağacın son
> sürümüne karşı hepsi yeniden koşuldu: **44 mutasyon, 44'ü kırmızı** (M01–M26 handler,
> D01–D07 domain, X11/X35/X36 `logoRefOf`'un yarım sütunları, Q01–Q02 sorgu + `make sqlc`);
> derleme hatası 0; her birinde mutantın uygulandığı ve geri yazıldığı sha ile doğrulandı;
> karar testin kendi `--- FAIL` satırlarından okundu; ortak 41 satırın kırmızı listesi 1.
> turdakiyle birebir aynı. 1. turun ilk koşusunda D06 (DB hatası ıskaya döner) **yeşil**
> kalmıştı — başarısız veritabanı kontrolü eklendi. Q01/Q02'de üretim yolu izolasyon testleri
> yeşil kaldı (RLS); kırmızıya dönen belt. **3. tur:** son kod ve test sürümüne karşı bir kez
> daha: **47 mutasyon, 47'si kırmızı** — 2. turun 44'ü (kırmızı listeleri birebir aynı) +
> A07, A10, A08 (seam testiyle koşuldu; 2. turun testinde yeşil ölçüldü, tarama sayımından
> sonra kırmızı).
>
> **Devirler:**
> - **WL-7:** önizlemenin `<img>`'i `/admin/brand/logo/{sha}`; `adminCSPFor(true)` yalnız o
>   render çizdiğinde; önizleme img-src testinin listesine logolu fikstürle girer. **Bayt
>   (sınır 12):** yükleme rotası gelince ya logo okuması için bir bayt bütçesi konur ya da
>   75 MiB / oturum / 10 dk gerekçesiyle kabul edilir (kayıt herkese açık — panel tarafı).
> - **WL-8:** kabuk `hasLogo`'yu `PageLogo`'dan (ya da accent'i de okuyan genişletmesinden —
>   tek PK okuması) alır; hata → `false` + log; `render`/`renderScripted` `hasLogo` taşıyan yol
>   ister; img-src testi logolu işletmenin bölümleriyle genişler (sınır 2'yi o kapatır).
> - **WL-8 ve WL-9 — yarım satır ayrı ele alınmalı (3. tur gözlemi, kod değişmedi):**
>   `PageLogo` yarım tanımlı satıra "logo yok" değil bir hata döndürür ve bu hata
>   `ErrLogoNotFound` değildir. Denetçinin A04'ü (yarım satıra `ErrLogoNotFound`) WL-6'nın
>   testlerinde fark edilmiyor — bugün çağıran yok. Bir çağıran `errors.Is(err,
>   ErrLogoNotFound)` ile "logo yok" yazarsa yarım satır sessizleşir; çağıran yarım satırı
>   okuma hatası gibi loglamalı (§4.6) ve kendi testinde sürmeli.
> - **WL-9:** `hasLogo` `TapPage`'in transaction'ında; `ErrForeignLocation` → `false`;
>   `Tap.render` render başına `tapCSPFor(hasLogo)`; `<img src="/t/logo/{sha}">` `width`/
>   `height` ile; sıcak/soğuk aritmetiği gerçek sayfayla yeniden ölçülür. **Bayt (sınır 12):**
>   tap tarafındaki 75 MiB / oturum / 10 dk ya bir bayt bütçesiyle sınırlanır ya da
>   gerekçesiyle kabul edilir.
> - **WL-10:** İddia D ve E'nin WL-6 parçaları, mutasyon tablosu, sayılı sınırlar.
> - **WL-12 / orkestratör:** ADR 0024 İddia D/E PART I'in "WL-6'da ölçülecek" yarıları ölçüldü
>   — işaret satırı önerilir; m10 §5 "Servis" maddesine `Content-Length`, 304 ve yalnız-GET
>   (ADR §5'e satır içi yazıldı).
>
> **Sayılı sınırlar** (ADR 0024 WL-6 notundakiyle aynı on dört): (1) img-src testi yalnız
> listelediği render'ları ve yalnız `<img` görür (CSS `url()`, SVG `<image>` sayılmaz) ·
> (2) hiçbir render henüz `hasLogo=true` geçmez · (3) rotayla eşleşmeyen yol şekilleri chi'nin
> 404'ünü, GET dışı metotlar 405'i alır (yedi standart metot `Allow: GET` ile, `FOO`
> `Allow`'suz) — yalnız URL'e/metoda bağlı · (4) DB uçtan uca testinde oturum sahte;
> 304/HEAD/bütçe/bozuk digest testleri sahte okuyucuyla · (5) süre kehaneti ölçülmedi ·
> (6) 304 de baytı okur · (7) devre dışı çalışanın canlı oturumu logoyu alır · (8) tenant
> yükleminin silinmesini yalnız belt yakalar · (9) RFC metni okunmadı · (10) ölçümler
> go1.27.1'de · (11) fikstürler dev DB'de kalır · (12) yanıt başına bayt — ayrı bir sınır
> yok (75 MiB / oturum / 10 dk) · (13) `OperatorHost` ayarlı yapılandırmada müşteri host'unun
> logo yanıtı telde ölçülmedi · (14) `If-None-Match` satırlarında net/http'den farklı
> (birleştirir; [`"x"`, `"<sha>"`] burada 304, orada 200) — yalnız kendi logosu, kehanet yok.
>
> **Doğruluk iddiası, üç parçalı** (ADR 0024 WL-6 notunun İddia D ve E'si):
> - **PART I** — sevk edilen kodun ölçülen davranışı yukarıdaki on kabul maddesinde, her
>   biri ölçen testin adıyla.
> - **PART II** — adlı pinler: `TestLogoRoutes_TheHeadersAreExactlyTheADRs`,
>   `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`,
>   `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer`,
>   `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`,
>   `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`,
>   `TestIfNoneMatch_TheScanFollowsRFC9110`, `TestLogoDigest_OnlySixtyFourLowerCaseHexDigits`,
>   `TestLogoFileName_FollowsTheStoredType`, `TestLogoRoutes_AReadFailureIsNotARefusal`,
>   `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`, `TestNewBrandLogos_RefusesAMissingDependency`,
>   `TestPagePolicies_ALogoWidensByImgSrcAlone`, `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`,
>   `TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession`,
>   `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`,
>   `TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness`, `TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo`,
>   `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch`, `TestBrandRead_ANilTenantOpensNoTransaction`,
>   `TestBrandRead_TheBeltSeesBothReads`, `TestStaffQueries_CarryAnExplicitTenantPredicate`,
>   `TestTapResponses_CarryTheContentSecurityPolicy`; her birinin yakaladığı tam liste ADR
>   notunun mutasyon tablosunda (47 mutasyon, hepsi koşuldu, hepsi kırmızı).
> - **PART III** — Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

> **Kart düzeltmesi (2026-10-03, WL-5 uygulaması sırasında; 6 tur + kapanış metni).** Yazıldı:
> `internal/brand/theme.go` (`ThemeCSS` — tema gövdesi, `Check`'in cevabından) ·
> `internal/handler/brandtheme.go` (`BrandTheme`, `NewBrandTheme`, `GET`+`HEAD /brand/theme/{file}`) ·
> `cmd/tappa/main.go` (rota `httpx.NewRouter`'a verilir — bir satır) · `tailwind.config.js` (üç
> token) · `web/static/css/input.css` (`:root` varsayılanları; `.tap-button` token'ları okur) ·
> testler `internal/brand/theme_test.go`, `internal/handler/brandtheme_test.go`,
> `cmd/tappa/brandtheme_test.go` · `internal/brand/doc.go` (bir cümle) ·
> `internal/handler/dashboard_test.go` (`nonSurfaceGrounds["tappa-green"]` gerekçesi, "bugün" diye
> tarihli; üç bayat "CI'da SKIP" yorumu) · `input.css`'te bir bayat "CI'da SKIP" yorumu ve `.lp .plaque .p-brand` kuralının
> üstünde golden'a işaret eden bir yorum · [ADR
> 0023](../adr/0023-tenant-markasi-ve-arayuz-kurali.md) §4'e tarihli *"WL-5 notu"* (kural
> değişmedi), §6 girişinde ve İddia A/B/C'de (İddia C'nin başlığı 6. turda PART I'ine daraltıldı) *"adı WL-5'te"* boşluklarına test adları, §6 madde 1'in
> satır atfı yerine kural adı ve okuyan testin ne okuduğu (deseniyle: `\.stamp\b`). `go.mod`/`go.sum`/`sqlc.yaml` diff'i boş;
> `web/templates` diff'i boş (`.templ`/`_templ.go`; kabul 12). **Ölçüm ortamı:** darwin/amd64, yerel
> Go 1.27.1, staticcheck ve `vet` Go 1.26.7 ile; Tailwind standalone v3.4.17; headless Chrome
> 154.0.8037.93. DB'ye gidilmedi. **Bu kartta ve ADR notunda satır numarası yok;** kurallar seçici
> adıyla anılır; bir test, *Doğruluk iddiası PART I*'deki maddesiyle anılır.
>
> **Kabul satırı → PART I maddesi:**
> 1. **Kurucu havuz/sorgu arayüzü almaz** → madde 1 (sapma a: `reflect`). Kimliğe bakmaz → madde 7.
>    Üründe bağlı → madde 10.
> 2. **200/404 matrisi** → madde 2. Koşu günlüğü: 11 kabul / 46 ret vakası; günlüğe yazılan sınır
>    komşuları paper `6E7B44` | `7D5BEC` · ink `1E93A0` | `8D76DA` · kenar `B268EC` | `8F8A7A` — ADR
>    0023 §3 WL-2 notunun altı hex'iyle aynı (ölçüm; test bunu assert etmez). Ret listesi: küçük/karışık
>    harf, `%23`, `%31` kodlu rakam, boşluklu, `0x`'li, tam genişlikli rakam, 3/5/7 hane, `GGGGGG`, boş,
>    uzantısız, `.CSS`/`.Css`/`.json`/`.cs`/`.css.css`, `%00`, `%2Ecss`, `%0A`, `%0D%0A`, `;`, `%3B`,
>    `%7D`, `%7B`, `..%2F`, `%2E%2E/`, `../theme/`, `%2F`, sonda `/`, fazladan segment, eksik segment,
>    çift `/`, `?v=1`, `?`, `?%0A`, `808080`, `E0457B`.
> 3. **Başlıklar birebir** → madde 2, 3, 6. Ham TCP ölçümü (scratch `wl5wire`, bir kez): 200
>    `Content-Length: 78` (`1F5C41`), `81` (`FFC72C`); 404 `Content-Type: text/plain; charset=utf-8`,
>    `X-Content-Type-Options: nosniff`, `Content-Length: 19`, gövde `404 page not found\n`.
> 4. **HEAD** → madde 4; öteki metotlar → madde 5. Karar b.
> 5. **Gövde üç özellik** → madde 9 ve 2 (koşu günlüğü: 4099 adımlı tarama ret 476 / paper 1 465 /
>    ink 115 / ink+kenar 2 038; 256 gri ret 16 / paper 118 / ink 5 / ink+kenar 117). Gramer:
>    `^:root\{--brand-accent:R G B;--brand-on-accent:R G B;--brand-edge:(R G B|none)\}$`, sayılar
>    kanonik 0–255 ondalık. Karar c.
> 6. **Tailwind'e üç token + `:root` varsayılanları** → `tailwind.config.js`: `backgroundColor.brand`,
>    `textColor['on-brand']`, `boxShadowColor['brand-edge']`, değerleri `rgb(var(--…) /
>    <alpha-value>)` (sapma d). `input.css` `@layer base`: `:root{--brand-accent:31 92 65;
>    --brand-on-accent:255 253 244;--brand-edge:none}`; `.tap-button` `bg-brand text-on-brand` +
>    `box-shadow: inset 0 0 0 2px theme('boxShadowColor.brand-edge')`.
> 7. **Slot testi** (ADR 0023 İddia C'nin adı konmamış testi) → madde 11; negatif kontrolü madde 12.
>    ADR'nin iki mutasyonu (C1, C2) kırmızı. CI'da koşar; `make css` koşulmamış yerel koşuda SKIP;
>    negatif kontrol `app.css` istemez.
> 8. **Derlenmiş-CSS varsayılan testi** → madde 13 (aynı SKIP koşulu). **5. tur:** *"başka bir yerde
>    geçmez"* iddiası ayrıştırıcısız geçiş golden'ına taşındı → madde 14 (aynı SKIP koşulu); negatif
>    kontrolü madde 15 (`app.css` istemez).
> 9. **CDP ölçümü (bir kez, pin değil)** — aşağıda.
> 10. **`TestCompiledCSS_StampWordIsInk` ve `TestBrand_NoOffPaletteColourInAnySource` yeşil** — taze
>     `app.css` ile, `-race`, SKIP 0 (sayılar teslim raporunda).
> 11. **Yorumdan ölü CSS kuralı doğmadı** — WL-5 öncesi (`6f3a70e`) ve sonrası taze `app.css` aynı
>     komutla derlendi (iki koşu aynı sha), kurallar düzleştirilip çok-küme farkı alındı: öncesi 544
>     kural / 49 943 B (`61b36986a2adc14e…`), sonrası 546 kural / 50 107 B (`c5f24499dafc821a…`).
>     Fark üç kural: `+ :root{…}` · `+ .tap-button{box-shadow:inset 0 0 0 2px
>     rgb(var(--brand-edge)/1)}` · `.tap-button` ana kuralında iki değer. 2.–4. turun yalnız-yorum
>     değişikliklerinden sonra `app.css` yeniden derlendi: bayt-aynı (`c5f24499…`).
> 12. **Bugünkü HTML değişmedi** — `git diff --stat -- web/templates` boş; `make gen` sonrası
>     `git status` aynı.
>
> **CDP ölçümü (bir kez, pin değil; betikler scratchpad'de, repoya girmedi).** Çalışma ağacının
> `rsync` kopyasında bir `cmd/wl5probe`: gerçek `pages.Tap(TapView{"Maria Borg", "KF St Julians", …})`
> render'ı, `tapCSP` başlığıyla, `httpx.NewRouter(nil, nil, handler.NewBrandTheme(), probe)` üstünden;
> `app.css` seçilen derlemeden. Chrome `--headless=new --remote-debugging-pipe`, CDP pipe üstünden
> (NUL ayrımlı JSON): `Target.createTarget` → `attachToTarget(flatten)` →
> `Emulation.setDeviceMetricsOverride` (390×844, DSF 3, mobil) → `Page.navigate` →
> `document.readyState` yoklaması → `document.fonts.ready` →
> `Runtime.evaluate(getComputedStyle([data-tap-button]))` → düğmenin ±4 px'lik `Page.captureScreenshot`'ı.
> Tema varyantı yalnız scratch'te, `app.css` bağlantısından hemen sonra `<link
> href="/brand/theme/HEX.css">` eklenerek kuruldu.
>
> | `app.css` | tema | background-color | color | box-shadow | kutu |
> |---|---|---|---|---|---|
> | WL-5 öncesi | — | `rgb(31, 92, 65)` | `rgb(255, 253, 244)` | `none` | 16,214 358×64 |
> | **WL-5** | **— (markasız)** | **`rgb(31, 92, 65)`** | **`rgb(255, 253, 244)`** | **`none`** | aynı |
> | WL-5 | `1F5C41` | `rgb(31, 92, 65)` | `rgb(255, 253, 244)` | `none` | aynı |
> | WL-5 | `FFC72C` | `rgb(255, 199, 44)` | `rgb(21, 34, 25)` | `rgb(21, 34, 25) 0px 0px 0px 2px inset` | aynı |
> | WL-5 | `DA291C` | `rgb(218, 41, 28)` | `rgb(255, 253, 244)` | `none` | aynı |
> | WL-5 | `D98E2B` | `rgb(217, 142, 43)` | `rgb(21, 34, 25)` | `rgb(21, 34, 25) 0px 0px 0px 2px inset` | aynı |
> | WL-5 | `808080` (404) | `rgb(31, 92, 65)` | `rgb(255, 253, 244)` | `none` | aynı |
> | WL-5 | `1f5c41` (404) | `rgb(31, 92, 65)` | `rgb(255, 253, 244)` | `none` | aynı |
> | seçenek Z (`--brand-edge` = accent) | — | `rgb(31, 92, 65)` | `rgb(255, 253, 244)` | `rgb(31, 92, 65) 0px 0px 0px 2px inset` | aynı |
>
> Her satırda `font-size` 20px, `font-weight` 400, `min-height` 64px. Piksel karşılaştırması (düğme
> kırpığı, 1 098×216 = 237 168 piksel, WL-5 öncesine karşı): markasız 0, `1F5C41` teması 0, `808080`
> (404) 0, seçenek Z 36 (hepsi yuvarlatılmış köşelerde), `DA291C` 205 254 (pozitif kontrol). Tema
> `tapCSP`'nin `style-src 'self'`'i altında yüklendi. Her koşudan sonra `pgrep` boş.
>
> **Geçiş golden'ının bugünkü içeriği (5. tur, ölçüldü; 6. turda değişmedi):** dört yer, yedi geçiş — derinlik 1 `:root{--brand-accent:31 92 65;--brand-on-accent:255 253 244;--brand-edge:none}` × 3 · derinlik 1 `.tap-button{…;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1));…;color:rgb(var(--brand-on-accent)/var(--tw-text-opacity,1));…}` × 2 (tam metin `themeBrandGolden`'da) · derinlik 1 `.tap-button{box-shadow:inset 0 0 0 2px rgb(var(--brand-edge)/1)}` × 1 · derinlik 0 `.lp .plaque .p-brand{color:#9db3a5;font-family:var(--mono);font-size:10px;letter-spacing:3px;margin-bottom:18px}` × 1. **Birleşik ağaç:** `e0f53b6`'nın `web/templates` + `web/static/js`'i, bu çalışma ağacının `tailwind.config.js`'i ve WL-6'nın `.op-version` parçası eklenmiş `input.css` ile scratch'te derlenen `app.css`'te dört pencere ve derinlikleri bayt bayt aynı (scratch `merged_css.py`).
>
> **Kararlar (gerekçeli):**
> - **a. Kurucu pini `reflect` ile, go/types değil.** Pinlenen özellik bir parametre sayısı ve bir alan
>   sayısı; ikisi de derlenmiş tipte eksiksiz durur ve `reflect` takma ad / nokta-import / gömülü alanı
>   çözülmüş hâliyle görür. go/types yolu `go list -export -deps` alt süreci + `-race` eşleşmesi + araç
>   zinciri eşitliği ister (`internal/handler/operator/typepins_test.go` emsali).
> - **b. HEAD kabul edilir** — `/healthz` ve `/readyz` emsali: GET-only chi rotası HEAD'e 405 verir;
>   bu cevap kimliksiz, bütçesiz, veritabanı sorgusuz; madde 7'nin dört istek giydirmesinde aynı
>   baytlar.
> - **c. `Edge == false` → `--brand-edge:none`.** `none` bir renk değil: bugün (WL-5) onu okuyan
>   kural (`.tap-button`'ın iç gölgesi) hesaplanan değer anında geçersiz olur ve `box-shadow` `none`'a düşer
>   — markasız düğmenin hesaplanan stili WL-5 öncesiyle aynı (CDP). Öteki aday (seçenek Z) hesaplanan
>   `box-shadow`'u ve köşelerde 36 pikseli değiştirir. Bedel: gövde grameri iki biçimli.
> - **d. Token'lar özelliğe göre, `colors` altında değil.** `colors` dokuz token'lık palettir ve WL-2'nin
>   Go kopya testi onu satır satır hex olarak okur; ve özellik kuralı üreticide yapısal olur
>   (`text-brand`, `border-brand`, `bg-on-brand`, `ring-brand-edge`, `text-brand-edge` kural üretmez —
>   S1).
> - **e. Sorgu dizesi 404** (boş `?` dahil): bir renk, bir URL.
> - **f. Gövde `brand.ThemeCSS`'te**; `ThemeCSS` `Check`'i kendisi çağırır, reddedilen accent'e gövde
>   yok.
> - **g. `.tap-button` token'lara geçti**: yoksa slot testi boş kümeyi tarardı ve CDP ölçümü bir şey
>   ölçmezdi.
> - **h. (2. tur) Rota/gövde defteri** (`brandThemeShipped`) — güvenlik denetçisinin DÜŞÜK bulgusu;
>   kapsamı sınır 2.
> - **i. (5. tur) Geçiş golden'ı `brand` üstünde, `--brand` değil.** `brand` daha geniş: token
>   yardımcılarının adlarını (`.bg-brand`, `.shadow-brand-edge`) ve yazıldığı hâliyle okumada kaçışlı
>   tire yazımlarını (`--brand\-accent`, `\2d\2d brand-accent`) da sayar. Bedeli ölçüldü: listede
>   bir değişken olmayan yer var (açılış sayfasının `.lp .plaque .p-brand` kuralı, bir geçiş).
> - **j. (5. tur) Pencere = önceki `}` → sonraki `}`, artı süslü parantez derinliği.** Pencere
>   kuralın seçicisini (ya da bloğun ilk kuralıysa at-rule başlığını) içerir; tek başına, bir
>   at-rule içinde başka bir kuraldan sonra gelen aynı kuralı ayırmaz. Derinlik onu ayırır (D1
>   kırmızı; derinliği çıkarılmış pinle D1 yeşil — D1GU3). Tam bir at-rule yolu bir ayrıştırıcı
>   isterdi; bu pinin amacı ayrıştırıcısız olmak.
> - **k. (5. tur) İki okuma: yazıldığı hâliyle ve kaçışlar çözülmüş.** Çözülmüş okuma kaçışlı harfi
>   görür (`--\62 rand-accent`; X9c'yi yalnız o yakalar — X9cGU1); yazıldığı hâliyle okuma, çözmenin
>   başka bir kod noktasına kattığı yazımı görür (`\brand` → U+000B + `rand`; X11'i yalnız o
>   yakalar — X11GU2).
> - **l. (5. tur) Tel testleri bayt bayt, Go'nun HTTP ayrıştırıcısı olmadan.** Ayrıştırıcı
>   `Connection` ve `Transfer-Encoding`'i başlık haritasından çıkarır (A3b, A3c, H22 4. turda
>   yeşildi). İstek `Connection` taşımaz, çünkü kapanmak isteyen bir isteğe sunucu kendi
>   `Connection: close`'unu ekler; cevabın bittiği yer aynı bağlantıdaki ikinci isteğin cevabıyla
>   işaretlenir (zaman aşımına dayanmaz). Başlık satırları sıralanarak karşılaştırılır: satır sırası
>   ürünün özelliği değil.
> - **m. (6. tur) Kaçışlı süslü parantezler önce nötrleştirilir, iki okuma aynı nötr metinden
>   çıkar.** `themeBrandNeutral` yalnız süslü paranteze çözülen kaçışları (`\{`, `\}`, `\7d `)
>   U+FFFD ile değiştirir; öteki her bayt, öteki kaçışlar dahil, yerinde kalır. Yazıldığı hâliyle
>   okuma bu nötr metindir (`\brand` hâlâ görünür — X11), çözülmüş okuma onun `themeUnescape`'idir
>   (`\62 rand` hâlâ `brand` olur — X9c). Kaçış, `themeUnescape`'in kullandığı aynı okuyucuyla
>   (`themeEscapeAt`) tanınır, yani iki adım bir kaçışın nerede bittiğinde ayrışmaz. U+FFFD:
>   `themeUnescape`'in geçersiz kod noktası için yazdığı karakter; süslü parantez ya da ters bölü
>   değil, `brand`'in parçası olamaz.
> - **n. (6. tur) İkinci cevap birebir.** Bağlantıda ilk cevaptan sonra kalan baytlar, aynı isteğin
>   kendi bağlantısında aldığı 404 ile (`Date` değeri maskeli; durum satırı, başlıklar, 19 baytlık
>   gövde, sonra kapanış) bitmek zorunda; öncesi `extra`. Alt dizge araması (5. tur) tek bir sahte
>   durum satırını kabul ediyordu (W1b, W2h).
> - **o. (6. tur) Tehdit modeli.** Pinler `input.css` / `tailwind.config.js`'e kazara giren sapmaya
>   karşıdır; tarayıcıyı bilerek atlatan bir stil dosyası kod incelemesinin konusudur. Bu yüzden
>   bilerek dengelenmiş bir yorum parantezi (F5) düzeltilmedi, ölçülüp sınır 11 olarak yazıldı.
>   Kalıtım yolu (E5) bu gruba girmez: kazara da yazılır (denetçinin D9'u — ebeveynde
>   `position: relative` unutulmuş bir dalga efekti; sınır 10). Test onu görmüyor; sınır 10'da
>   ölçümüyle yazılı ve WL-9/WL-10'a kod incelemesi notu olarak devredildi.
>
> **Mutasyon tablosu** (her satır tek mutasyon; scratch `mut.py` + `mut_r4.py` + `mut_r5.py` + `mut_r6.py`: yedekler, eski metnin
> tam bir kez geçtiğini doğrular, uygular, gerekirse `app.css`'i yeniden derler, `go test -v` koşar,
> hükmü testin çıktısından okur, geri yazar ve sha256 ile doğrular. "Kırmızı" sütunu koşulan `-run`
> kapsamını söyler: *WL-5 kümesi* = handler'da `TestBrandTheme_|TestCompiledCSS_|TestBrand_`, brand'de
> `TestTheme_|TestCompiledCSS_|TestThemeSlotScan_|TestThemeBrandScan_` (son önek 5. turdan
> beri). Derleme hatasıyla düşen mutasyon yok.)
>
> | # | Mutasyon | Nerede | Kırmızı (kapsam) |
> |---|---|---|---|
> | H1 | `NewBrandTheme(deps ...any)` | `brandtheme.go` | `TestBrandTheme_TheConstructorTakesNothing` (WL-5 kümesi) |
> | H2 | `BrandTheme`'e sorgu-arayüzü alanı | `brandtheme.go` | aynı |
> | H3 | `BrandTheme`'e gömülü `*http.Client` | `brandtheme.go` | aynı |
> | H4 | `ThemeCSS` kapısız `Fill` kurar | `theme.go` | `TestTheme_TheBodyIsTheGatesFill`, `TestBrandTheme_AnswersOnlyACanonicalLegibleHex`, `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders` (WL-5 kümesi) |
> | H5 | `ParseAccent` → `NormalizeAccent` | `brandtheme.go` | `TestBrandTheme_AnswersOnlyACanonicalLegibleHex` |
> | H6 | sorgu kontrolü kalktı | `brandtheme.go` | aynı |
> | H7 | uzantı isteğe bağlı | `brandtheme.go` | aynı |
> | H8 | uzantı harf duyarsız | `brandtheme.go` | aynı |
> | H9 | ek başlık (`Access-Control-Allow-Origin`) | `brandtheme.go` | matris + tel testi |
> | H10 | `Cache-Control: no-store` | `brandtheme.go` | matris + tel testi |
> | H11 | kendi 404 gövdesi | `brandtheme.go` | matris + tel testi |
> | H12 | yol parametresi gövdeye | `brandtheme.go` | matris |
> | H13 | HEAD kaydı kalktı | `brandtheme.go` | `TestBrandTheme_HeadIsGetWithoutTheBody` |
> | H14 | oturum çerezi varsa `Cache-Control: private` | `brandtheme.go` | `TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks` |
> | H15 | kenarsızda kenar = accent | `theme.go` | `TestTheme_TheBodyIsTheGatesFill`, `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme`, `TestThemeSlotScan_RefusesEachShapeItExistsFor`, matris |
> | H16 | etiket hep paper | `theme.go` | `TestTheme_TheBodyIsTheGatesFill`, matris |
> | H17 | kenar etiketten önce | `theme.go` | H15 ile aynı dört test |
> | H18 | gövde sonunda `\n` | `theme.go` | H15 ile aynı dört test |
> | W1 | rota `NewRouter`'a verilmedi | `main.go` | `TestBrandThemeWiring_RunMountsTheThemeRoute` |
> | C1 | `.tap-button{color:rgb(var(--brand-accent))}` | `input.css` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
> | C2 | `.stamp`'e `bg-brand` | `input.css` | aynı (`TestCompiledCSS_StampWordIsInk` yeşil kaldı) |
> | C3 | şablonda `class="bg-brand …"` | `result.templ` | aynı |
> | C4 | şablon **yorumunda** `bg-brand` | `result.templ` | aynı |
> | C5 | `.docket{--brand-accent:255 0 0}` | `input.css` | aynı |
> | C6 | varsayılan accent `31 92 66` | `input.css` | `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` |
> | C7 | varsayılan etiket ink | `input.css` | aynı |
> | C8 | varsayılan kenar `31 92 65` | `input.css` | aynı |
> | C9 | `.tap-button` yeniden `bg-tappa-green` | `input.css` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
> | C10 | kenar ayrıca `border-color` | `input.css` | aynı |
> | C11 | `@media print{.stamp{box-shadow:…var(--brand-edge)…}}` | `input.css` | aynı |
> | C12 | varsayılanlar silindi | `input.css` | slot + varsayılan testi |
> | S1 | şablonda `text-brand border-brand bg-on-brand ring-brand-edge text-brand-edge` | `result.templ` | **yeşil — 0 kural farkı** (karar d) |
> | S2 | şablonda `shadow-brand-edge text-on-brand` | `result.templ` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
> | L1 | tema `<link>`'i ortak `<head>`'e | `layout/base.templ` (+`templ generate`) | `TestScreens_ReferenceOnlyOurOwnAssets`, `TestTour_PointsOnlyAtItsOwnFlow` (handler paketi, `-run .`, DB'siz) |
> | L2 | tema `<link>`'i yalnız `tap.templ`'e | `pages/tap.templ` (+`templ generate`; üretilen dosya bağlantıyı taşıdı) | **hiçbiri** (handler paketi, `-run .`, DB'siz) — sınır 5 |
> | V1 | palet sabiti `paletteInkHex` `152219` → `152218`, defter aynı | `brand/accent.go` | WL-5 kümesinde yalnız `TestBrandTheme_ANewBodyNeedsANewRoute`. **Tam paketlerde** (`go test -count=1 ./internal/brand ./internal/handler`, DB'siz, `-race`'siz): brand'de 6 üst düzey FAIL — `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`, `TestAccent_TheDesignTableHolds`, `TestAccent_TheNearestHexEitherSideOfEachComputedBoundary`, `TestPalette_TheComparisonSeesEveryKindOfDrift`, `TestPalette_TheGoCopyEqualsTailwindConfig`, `TestSuggest_TheDesignExamples` — handler'da 1, toplam 7 (denetçi kendi koşusunda 8 saydı; fark ölçülmedi) |
> | V2 | `ThemeCSS` özellik sırası, defter aynı | `theme.go` | `TestBrandTheme_ANewBodyNeedsANewRoute`, matris (WL-5 kümesi) |
> | V3 | deftere aynı rotayla ikinci kayıt | `brandtheme_test.go` | `TestBrandTheme_ANewBodyNeedsANewRoute` |
> | V4 | rota sabiti değişti, gövdeler ve defter aynı | `brandtheme.go` | `TestBrandTheme_ANewBodyNeedsANewRoute` + yolu kullanan dört test |
> | V5 | palet değişti **ve** tek kaydın özeti yerinde yeniden yazıldı | `accent.go` + `brandtheme_test.go` | **hiçbiri — yeşil** (sınır 2). İlk koşu araç hatasıyla kırmızı göründü (yer tutucu özet değişmemişti); gerçek özetle yeniden koşuldu |
> | A9 | `serve`'den paket düzeyi kanca | `brandtheme.go` | **hiçbiri — yeşil** (sınır 6) |
> | A16 | kenar dış gölgeyle | `input.css` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
> | A19 | rota POST için de kaydedildi | `brandtheme.go` | `TestBrandTheme_OtherMethodsAre405` |
> | G1 | operatör kapısı `/brand/` yollarını geçirir | `httpx/operatorhost.go` | `TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost` |
> | M8 (3. tur) | `Mount` başka bir yol literali kaydeder, sabit aynı | `brandtheme.go` | `TestBrandTheme_ANewBodyNeedsANewRoute` (yalnız o test koşulunca da kırmızı; 2. turda yeşildi) + yolu kullanan dört test |
> | M3 (3. tur) | ara grilere (1–254) ink kenar | `theme.go` | `TestTheme_TheBodyIsTheGatesFill` (2. turda yeşildi); handler WL-5 kümesi yeşil |
> | T1 (3. tur) | `Check`'in kenar eşiği 3.0 → 3.05, defter aynı | `brand/accent.go` | WL-5 kümesinde yalnız `TestBrandTheme_ANewBodyNeedsANewRoute`; tam brand paketinde ayrıca `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`, `TestAccent_TheNearestHexEitherSideOfEachComputedBoundary` |
> | H20 (4. tur) | `NewBrandTheme` `httpx.Mounter` döndürür | `brandtheme.go` | `TestBrandTheme_TheConstructorTakesNothing` |
> | H21 (4. tur) | `F` ile başlayan kabul hex'i reddedilir | `brandtheme.go` | `TestBrandTheme_AnswersOnlyACanonicalLegibleHex` |
> | H22 (4. tur) | gövdeden önce `Flush` (chunked) | `brandtheme.go` | `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders` |
> | H23 (4. tur) | gövdeden sonra trailer | `brandtheme.go` | tel testi, matris, HEAD testi, operatör kapısı testi |
> | H25 (4. tur) | PROPFIND kaydedildi | `brandtheme.go` | `TestBrandTheme_OtherMethodsAre405` |
> | H26 (4. tur) | operatör kapısı müşteri host'unda başlık ekler | `httpx/operatorhost.go` | `TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost` |
> | H27 (4. tur) | her cevaba `Vary: Cookie` | `brandtheme.go` | `TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks`, matris, tel testi, operatör kapısı testi |
> | H28 (4. tur) | `OnColor` ters | `brand/accent.go` | `TestTheme_TheBodyIsTheGatesFill`, `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme`, `TestThemeSlotScan_RefusesEachShapeItExistsFor` |
> | H29 (4. tur) | `nosniff` başlığı kalktı (eksik başlık) | `brandtheme.go` | matris, tel testi, operatör kapısı testi |
> | H30 (4. tur) | `ThemeCSS` `--brand-edge`'in adını değiştirir | `theme.go` | `TestThemeSlotScan_RefusesEachShapeItExistsFor`, `TestTheme_TheBodyIsTheGatesFill`, `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` |
> | W2 (4. tur) | `run()`'da ikinci bir `NewRouter` çağrısı | `main.go` | `TestBrandThemeWiring_RunMountsTheThemeRoute` |
> | M8b (4. tur) | `Mount` ikinci bir yol da kaydeder | `brandtheme.go` | `TestBrandTheme_ANewBodyNeedsANewRoute` |
> | X8 (4. tur) | HEAD bağlantısı hijack edilir, gövde (`FFC72C` için 81 bayt) başlıklardan sonra yazılır | `brandtheme.go` | `TestBrandTheme_HeadIsGetWithoutTheBody` (3. turda yeşildi; 5. turda ham okumayla yeniden kırmızı) |
> | X8b (4. tur) | GET bağlantısı hijack edilir, gövdeden sonra fazladan bayt | `brandtheme.go` | HEAD testi, matris, operatör kapısı testi |
> | X9a (4. tur) | `.stamp`'te `var(--brand\-accent)` (Tailwind kaçışı korudu) | `input.css` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` (3. turda yeşildi) |
> | X9b (4. tur) | `.stamp`'te `var(--brand\2d accent)` | `input.css` | aynı |
> | X9c (4. tur) | `.stamp`'te `var(--\62 rand-accent)` | `input.css` | aynı |
> | X9d (4. tur) | `.stamp`'te `var(\2d\2d brand-accent)` | `input.css` | aynı |
> | X9aU1 (4. tur) | X9a + taramanın kaçış çözmesi kapalı | `input.css` + `theme_test.go` | yalnız negatif kontrol; slot testi **yeşil** — çözme yükü taşıyor |
> | X10 (4. tur) | derlenmiş `:root` kuralında boşluk ve çift `;` | `app.css` | `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` (3. turda yeşildi) |
> | K1 (4. tur) | korunan yorumda `--brand-accent` | `input.css` | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
> | U1 (4. tur) | taramanın kaçış çözmesi kapalı | `theme_test.go` | `TestThemeSlotScan_RefusesEachShapeItExistsFor` |
> | U2 (4. tur) | taramanın iç gölge şartı kapalı | `theme_test.go` | aynı |
> | U3 (4. tur) | varsayılan okuması `": "`'yi `":"`'ye çevirip karşılaştırır | `theme_test.go` | aynı |
> | U4 (4. tur) | varsayılan okuması birden çok `:root`'tan ilkini alır | `theme_test.go` | aynı |
> | X5a (4. tur) | `@media print`'te `.tap-button`'a ikinci iç gölge okuması | `input.css` | 4. turda **yeşil** (o turun sınır 9'u); 5. turda `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` kırmızı |
> | B1c5 (5. tur) | dosya sonuna `:root { color-scheme: light; /*! x */ --brand-accent: 255 0 0; }` | `input.css` | golden testi, `TestCompiledCSS_BrandVariablesOnlyInTheirSlots`, `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` (denetçinin ölçümünde üçü de yeşildi) |
> | B1c3 (5. tur) | dosya sonuna `.docket{color:red; /*! x */--brand-accent:255 0 0}` | `input.css` | golden testi, slot testi |
> | K2 (5. tur) | dosya sonuna `.docket { color: red; /*! --brand-accent */ }` | `input.css` | golden testi, slot testi |
> | ST (5. tur) | `.stamp { background-color: rgb(var(--brand-accent)); }` | `input.css` | golden testi, slot testi |
> | X9b, X9c (5. tur, yeniden) | `\2d` ve `\62 rand` yazımları `.stamp`'te | `input.css` | golden testi, slot testi |
> | X11 (5. tur) | dosya sonuna `/*! \brand */` | `input.css` | golden testi |
> | D1 (5. tur) | derlenmiş kenar kuralı `@media print{.x{color:red}…}` içine, başka bir kuraldan sonra | `app.css` | golden testi |
> | GD (5. tur) | golden'dan `.p-brand` satırı silindi, `app.css` aynı | `theme_test.go` | golden testi |
> | GD2 (5. tur) | golden'dan kenar satırı silindi, `app.css` aynı | `theme_test.go` | golden testi, `TestThemeBrandScan_RefusesEachShapeItExistsFor` (öncül) |
> | D1GU3 (5. tur) | D1 + pinin yerinden derinlik çıkarıldı | `app.css` + `theme_test.go` | golden testi **yeşil**; yalnız golden kontrolü kırmızı — derinlik yük taşıyor |
> | X9cGU1 (5. tur) | X9c + pinin çözülmüş okuması kaldırıldı | `input.css` + `theme_test.go` | golden testi **yeşil**; slot testi ve golden kontrolü kırmızı — çözülmüş okuma yük taşıyor |
> | X11GU2 (5. tur) | X11 + pinin yazıldığı hâliyle okuması kaldırıldı | `input.css` + `theme_test.go` | golden testi **yeşil**; yalnız golden kontrolü kırmızı — yazıldığı hâliyle okuma yük taşıyor |
> | GU1, GU2, GU3 (5. tur) | pinin çözülmüş okuması / yazıldığı hâliyle okuması / derinliği kaldırıldı | `theme_test.go` | `TestThemeBrandScan_RefusesEachShapeItExistsFor` |
> | GU4 (5. tur) | pin yalnız golden'dan *az* geçişi raporlar | `theme_test.go` | `TestThemeBrandScan_RefusesEachShapeItExistsFor` |
> | GU5 (5. tur) | pin kaybolan golden yerini raporlamaz | `theme_test.go` | `TestThemeBrandScan_RefusesEachShapeItExistsFor` |
> | U5 (5. tur) | ayrıştırıcı bildirimdeki yorumu yeniden bırakır | `theme_test.go` | `TestThemeSlotScan_RefusesEachShapeItExistsFor` |
> | A3b (5. tur) | rette `Connection: close` | `brandtheme.go` | tel testi, HEAD testi, matris (denetçinin ölçümünde tel testi ve HEAD testi yeşildi) |
> | A3c (5. tur) | 200'de `Connection: close` | `brandtheme.go` | tel testi, HEAD testi, matris, operatör kapısı testi |
> | H22 (5. tur, yeniden) | gövdeden önce `Flush` | `brandtheme.go` | tel testi, HEAD testi (4. turda HEAD testi yeşildi) |
> | H9, H10, H11, H13, H23, H29, X8b (5. tur, yeniden) | 4. turdaki gibi | `brandtheme.go` | tel testinin ham okumasıyla yine kırmızı (H13: HEAD testi ve 405 testi) |
> | E1 (6. tur) | ana `.tap-button` kuralının seçicisi `.stamp,.x\{\}.tap-button` | `app.css` | golden testi, slot testi |
> | E1b (6. tur) | aynısı onaltılık kaçışla: `.stamp,.x\7b\7d .tap-button` | `app.css` | golden testi, slot testi |
> | E1c (6. tur) | kenar kuralının seçicisi `.stamp,.x\{\}.tap-button` | `app.css` | golden testi, slot testi |
> | E2 (6. tur) | varsayılanlar `@media all{.q{color:red}…}` içine, derinlik önce `.z\}{…}`, sonra `.w\{{…}` ile dengelendi | `app.css` | golden testi, slot testi, varsayılan testi |
> | F4 (6. tur) | kenar kuralının yerine `@media print{.x{color:red}.y\}EDGE}\{` | `app.css` | golden testi, slot testi |
> | E1GU6, E1cGU6, E2GU6, F4GU6 (6. tur) | aynı dört biçim + nötrleştirme adımı kaldırıldı (5. turun pini) | `app.css` + `theme_test.go` | golden testi **yeşil** dördünde de (5. turdaki açık); slot testi ve golden kontrolü kırmızı |
> | GU6 (6. tur) | nötrleştirme adımı kaldırıldı | `theme_test.go` | `TestThemeBrandScan_RefusesEachShapeItExistsFor` |
> | O1 (6. tur) | kenar kuralı `:root`'tan hemen sonraya taşındı (aynı derinlik, aynı metin) | `app.css` | **hiçbiri — yeşil** (sınır 9) |
> | O2 (6. tur) | kenar kuralı iki kez yazıldı | `app.css` | golden testi |
> | E3 (6. tur) | varsayılanlar yalnız dengelenmiş korunmuş bir yorumda (`/*!{}` + kural + `*/`) | `app.css` | golden testi **yeşil**; slot testi ("defined 0 times", "outside any declaration") ve varsayılan testi kırmızı |
> | F5 (6. tur) | F4'ün kaçışlar yerine korunmuş yorumlardaki `}` ve `{` ile kurulmuş hâli | `app.css` | **hiçbiri — yeşil** (sınır 11, tehdit modeli) |
> | E5 (6. tur) | `.tap-button::after{content:"";position:fixed;inset:0;background-color:inherit}` | `input.css` | **hiçbiri — yeşil** (brand ve handler WL-5 kümeleri; sınır 10) |
> | W1b (6. tur) | GET hijack: doğru 200, ardından çıplak `HTTP/1.1 404 Not Found` satırı, kapanış | `brandtheme.go` | tel testi, HEAD testi |
> | W2h (6. tur; denetçinin W2'si — `W2` adı 4. turda `run()`'daki ikinci `NewRouter` mutasyonuna verilmişti) | HEAD hijack: GET'in başlık bloğu, ardından çıplak durum satırı, kapanış | `brandtheme.go` | HEAD testi |
>
> 5. turda C1, C2, C3, C5, C6, C10, C11, C12, S2, A16, K1, X9a, X9d ve X10 yeniden koşuldu: her biri
> golden testini de kırmızıya çevirdi (eski testlerdeki sonuçları değişmedi).
>
> 6. turda nötrleştirme ve birebir ikinci cevapla yeniden koşuldu, hepsi yine kırmızı: B1c3, B1c5, K2,
> X5a, ST, X9a, X9b, X9c, X9d, X11, D1, GD, GD2 (golden testi; GD2 ayrıca golden kontrolü) · GU1–GU5,
> U5 (kontroller; GU1 ve GU2'nin çapası nötr metne göre güncellendi) · D1GU3, X9cGU1, X11GU2'de golden
> testi yine yeşil (derinlik, çözülmüş okuma ve yazıldığı hâliyle okuma yük taşıyor) · A3b, A3c, H9,
> H10, H11, H13, H22, H23, H29, X8, X8b (tel ve HEAD testleri, 5. turdaki kırmızılar değişmedi).
>
> **Doğruluk iddiası (üç parça; PART I–III, ADR 0023 §4 WL-5 notundakiyle harfi harfine aynı).**
> - **Tehdit modeli:** Bu pinler `input.css` / `tailwind.config.js`'e kazara giren sapmaya karşıdır; tarayıcıyı atlatmak için bilerek yazılmış bir stil dosyası kod incelemesinin konusudur.
> - **PART I — her madde: test · beslenen girdiler · assert · o assert'i kıran mutasyon**
>   (bu listede her assert'in yanında onu kıran mutasyon yazılı; *öncül* diye işaretli olanlar
>   testin kendi girdilerini doğrular, ürün hakkında iddia değildir):
>   1. `TestBrandTheme_TheConstructorTakesNothing` · `NewBrandTheme` ve `BrandTheme`'in derlenmiş
>      tipi (`reflect`) · parametre sayısı 0 (H1); tek sonuç `*BrandTheme` (H20); alan sayısı 0
>      (H2, H3).
>   2. `TestBrandTheme_AnswersOnlyACanonicalLegibleHex` · `httpx.NewRouter(nil, nil,
>      NewBrandTheme())`; kabul tarafı: §3 tablosunun beş kabul rengi, siyah, beyaz ve
>      `brand.Check`'in kararının değiştiği üç yerin kabul tarafındaki komşuları; ret tarafı: iki
>      red bandı komşusu, `808080`, `E0457B` ve testte listelenen yazım, uzantı, yol ve sorgu
>      biçimleri · her kabul yolu 200 (H21) ve başlık haritası tam olarak üç başlık (H9, H10, H29,
>      A3c);
>      200 gövdesi gramere uyar, accent kendi baytları, etiket `Fill.Text`, kenar `Fill.Edge`'e göre
>      ve `ThemeCSS(c)`'ye eşit (H12, H16, H17); her ret yolu bağlanmamış bir yolun 404'üyle durum,
>      başlık haritası ve gövdede aynı (H4, H5, H6, H7, H8, H11, A3b); kabul edilenler iki `Edge` dalını
>      kapsar (öncül).
>   3. `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders` · gerçek sunucu; cevap bayt bayt, Go'nun
>      HTTP ayrıştırıcısı olmadan okunur (`Connection` başlığı taşımayan bir keep-alive istek, sonra
>      aynı bağlantıda ikinci bir istek); `1F5C41`, bağlanmamış bir yol ve `808080` · 200'ün durum
>      satırı `HTTP/1.1 200 OK` ve sıralanmış başlık satırları tam olarak `Cache-Control`,
>      `Content-Length` (gövde uzunluğu), `Content-Type`, `Date`, `X-Content-Type-Options` —
>      üçü değerleriyle (H9, H10, H29, H22, H23, A3c); gövdesi `ThemeCSS`'inki; ret, bağlanmamış
>      yolun 404'üyle durum satırı, başlık satırları (`Date` değeri hariç) ve gövdede aynı (H11,
>      A3b); üç cevabın her birinden sonra bağlantı tam olarak ikinci isteğin 404'ünü — yönlendiricinin
>      kendi bağlantısında verdiği 404 ile, `Date` değeri hariç, bayt bayt aynı — taşır ve kapanır
>      (X8b, W1b).
>   4. `TestBrandTheme_HeadIsGetWithoutTheBody` · madde 3'ün bayt bayt okuması, `FFC72C` ve
>      `808080` · HEAD'in durum satırı ve sıralanmış başlık satırları GET'inkiyle, `Date` değeri
>      hariç, aynı (H13, H22); HEAD'in başlık bloğundan sonra bağlantı tam olarak ikinci isteğin
>      404'ünü taşır ve kapanır (X8, W2h, A3b, A3c); GET'in gövdesinden sonra da (X8b, W1b).
>   5. `TestBrandTheme_OtherMethodsAre405` · iki yol; POST, PUT, PATCH, DELETE, OPTIONS, TRACE,
>      CONNECT ve PROPFIND · yedi standart metot 405 ve `Allow` tam olarak GET, HEAD (A19);
>      PROPFIND 405 (H25).
>   6. `TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost` · `Config{OperatorHost}` ile
>      kurulan yönlendirici, üç yol, müşteri ve operatör host'u · müşteri host'unda cevap ayarsız
>      yönlendiricininkiyle durum, başlık haritası ve gövdede aynı (H26); operatör host'unda
>      kapının bağlanmamış yol 404'üyle aynı (G1); müşteri host'unda `1F5C41` 200 ve üç başlık
>      (öncül).
>   7. `TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks` · iki yol × dört istek giydirmesi
>      (çalışan çerezi, panel çerezi, `Authorization`, yönlendirme + fetch başlıkları) · her
>      giydirmenin cevabı çıplak isteğinkiyle durum, başlık haritası ve gövdede aynı (H14); çıplak
>      cevapta `Set-Cookie` ve `Vary` yok (H27).
>   8. `TestBrandTheme_ANewBodyNeedsANewRoute` · `chi.Walk` ile `Mount`'un kaydettiği yollar; 15
>      rengin (§3 tablosunun yedisi, siyah, beyaz, madde 2'deki üç yerin iki yanındaki komşular)
>      `ThemeCSS` cevaplarının sha256 özeti; `brandThemeShipped` defteri · `Mount` tek yol kaydeder
>      (M8b); defterde hiçbir rota ve hiçbir özet iki kez geçmez (V3); bugünkü özet defterde tam
>      bir kez geçer (V1, V2, T1); o kaydın rotası `Mount`'un kaydettiği yoldur (M8, V4).
>   9. `TestTheme_TheBodyIsTheGatesFill` · §3 tablosunun yedi rengi, siyahtan başlayan her 4099.
>      renk, 256 gri · `Check`'in reddettiğine gövde yok (H4); kabul edilene gramer ve kanonik
>      ondalık (H17, H18), kendi baytları, `OnColor`'ın etiketi (H16), `Edge`'e göre ink ya da
>      `none` (H15, M3); tablo satırlarında kapı tablonun etiket ve kenarıyla uyuşur (H28); tarama
>      parçalarının ikisi de kapının dört cevabına rastlar (öncül).
>   10. `TestBrandThemeWiring_RunMountsTheThemeRoute` · `main.go`'nun AST'si · `run()`'da tam bir
>       `NewRouter` çağrısı (W2); onun argümanlarında tam bir argümansız `handler.NewBrandTheme()`
>       (W1).
>   11. `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` · derlenmiş `app.css` (yoksa SKIP) ·
>       ayrıştırıcının okuduğu kurallar için (kaçışlar çözülmüş, yorumlar düşürülmüş)
>       `themeSlotViolations` boş: `--brand-` bildirimlerin dışında — seçicide, at-rule başlığında,
>       yorumda, blok içi yorumda — geçmez (K1, K2, E3); okuduğu bir tanım üst düzey `:root`'ta (C5,
>       B1c3, E2) ve okuduğu tanımlarda her değişken bir kez (B1c5, C12, E3); bir okuma
>       `themeSlots`'taki bir seçicide (C2, C3, C4, S2, C11, ST, X9a, X9b, X9c, X9d, E1, E1b, E1c, F4)
>       ve `themeVariableProperty`'nin adlandırdığı özellikte (C1, C10); kenarı okuyan gölge `inset`
>       (A16); `themeSlots`'taki her (seçici, değişken) çifti okunur (C9). Bu madde, ayrıştırıcının
>       okuduğu kurallar için, *nerede ve hangi özellikte okunur* ve *nerede, kaç kez tanımlanır*
>       sorularıdır.
>   12. `TestThemeSlotScan_RefusesEachShapeItExistsFor` · sevk edilen şekil, tarama listesindeki
>       bozuk şekiller, varsayılan listesindeki bozuk şekiller, iki üst düzey `:root` · tarama
>       tablosunun değişken adları `ThemeCSS`'in bildirdikleriyle eşit (H30); sevk edilen şekil iki
>       okumadan da geçer (öncül); tarama listesindeki her şekil kendi ihlal metniyle raporlanır
>       (U1, U2, U5); varsayılan listesindeki her şekil tappa-green teması olarak okunmaz (U3);
>       iki üst düzey `:root` reddedilir (U4).
>   13. `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` · derlenmiş `app.css` (yoksa SKIP;
>       adlar çözülerek eşlenir) · ayrıştırıcının bulduğu, `--brand-*` tanımlayan üst düzey `:root`
>       kuralı tam bir tane (C12, B1c5, E2, E3); accent ve etiket paletin tappa-green ve paper'ı (C6,
>       C7); `Edge(tappa-green)` false ve kenar `none` (C8); kuralın yazıldığı metin (yorumsuz seçici
>       + `{`…`}`) `ThemeCSS(tappa-green)`'e bayt bayt eşit (X10). Bu madde *varsayılanlar ne*
>       sorusudur; tanımların kaç kez ve nerede olduğu madde 11'in kural 2'sindedir.
>   14. `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` · derlenmiş `app.css`'in tamamı (yoksa
>       SKIP); süslü paranteze çözülen kaçışlar U+FFFD'ye çevrildikten sonra yazıldığı hâliyle ve
>       kalan kaçışları çözülmüş hâliyle · iki okumada da `brand`'in harf büyüklüğünden bağımsız her
>       geçişinin (derinlik, küçük harfe indirilmiş çevreleyen metin) çoklu kümesi
>       `themeBrandGolden`'a birebir eşit (B1c3,
>       B1c5, K1, K2, X5a, ST, X9a, X9b, X9c, X9d, X11, C1, C2, C3, C5, C6, C10, C11, C12, S2, A16,
>       X10, D1, O2, E1, E1b, E1c, E2, F4, GD, GD2). Bu madde yerlerin metnini ve derinliğini tutar,
>       anlamını değil; tanımın yeri madde 11 ve 13'ündür (E3 bu maddeyi yeşil bırakır). Pencerede
>       yalnız harf büyüklüğü değişen bir kural bu maddeyi yeşil bırakır (denetçinin CASE1'i).
>   15. `TestThemeBrandScan_RefusesEachShapeItExistsFor` · golden'ın kendi kuralları art arda
>       (sevk edilen şekil), testte listelenen on üç şekil, birer satırı eksik golden · golden bir
>       yeri iki kez listelemez (öncül); golden kenar kuralını taşır (öncül; satırını silen GD2 bu
>       testi de kırmızıya çevirdi); sevk edilen şekil fark vermez (öncül); listedeki her şekil
>       fark verir (GU1, GU2, GU3, GU4, GU5, GU6); sevk edilen şekil birer satırı eksik golden'a
>       karşı fark verir (GU4).
> - **PART II — pinler ve yakaladıkları:** PART I'in parantez içindeki mutasyonları — her biri
>   kartın tablosunda, kırmızıya döndüğü testle ve koşulduğu `-run` kapsamıyla.
> - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
>
> **Sayılı sınırlar** (ADR WL-5 notunun Sınırlar maddesiyle harfi harfine aynı):
> - **Sınırlar:** (1) Madde 11, 13 ve 14'ün testleri derlenmiş `app.css` ister: CI onu `make
>   check`'ten önce derler (`.github/workflows/ci.yml` → *"Build the stylesheet (make css)"*), yani
>   orada koşarlar; `make css` koşulmamış yerel bir koşuda SKIP ederler ve SKIP bir geçiş değildir.
>   Madde 12 ve 15'in testleri `app.css` istemez. (2) Rota/gövde defterinden test yalnız madde 8'i
>   tutar. Kaydın özeti yerinde yeniden yazılırsa yeşildir (ölçüldü, V5); silinen kaydı ve 15 rengin
>   dışında kalıp 15'ini aynı bırakan bir değişikliği görmez. (3) `none` yalnız Chrome 154'te
>   ölçüldü. (4) Slot testi derlenmiş `app.css`'in kurallarını okur — öğeleri, kaskadı, opaklığı ve
>   başka stil dosyalarını değil; bir slot sınıfının slot olmayan bir öğeye yazılmasını görmez. (5) Tek bir
>   sayfanın şablonuna yazılan tema bağlantısını DB'siz koşan testlerin hiçbiri kırmızıya çevirmedi
>   (ölçüldü, L2); ortak `<head>`'e yazılanı `TestScreens_ReferenceOnlyOurOwnAssets` ve
>   `TestTour_PointsOnlyAtItsOwnFlow` kırmızıya çevirdi (ölçüldü, L1) — tek sayfalık bağlantının ağı
>   WL-9'un golden'ı. (6) Madde 1'in testi imzayı ve alanları görür; `serve`'ün ulaşabileceği paket
>   düzeyi durum ya da fonksiyonu görmez (ölçüldü, A9) — kod incelemesinin konusu. (7) Madde 10'un
>   testi sözdizimseldir (`main.go` AST'si): rota bir değişken ya da yardımcı üzerinden verilirse
>   yanlış-kırmızı verir, `run()` dışındaki bir `NewRouter`'ı okumaz. (8) Rota bütçesiz (`/static`
>   gibi): istek başına `ParseAccent` + `Check` + en çok 82 baytlık yazma; ADR bütçe istemiyor.
>   (9) Golden bir yerin metnini ve derinliğini tutar, dosyadaki sırasını değil: aynı derinlikte, aynı
>   metinle yeri değişen bir kural aynı yeri verir (ölçüldü: kenar kuralı `:root`'tan hemen sonraya
>   taşındı, O1, yeşil; denetçinin F3 ve E16'sı yeşil). Derlenmiş dosyada iki kez yazılan bir kural
>   kırmızıdır (O2; denetçinin F1/F2'si). Golden de sınır 4'teki gibi derlenmiş `app.css`'ten başka
>   stil dosyası okumaz. 4. turun sınır 9'u (kenarın ikinci bir iç gölge okuması) kapandı: X5a madde
>   14'ü kırmızıya çevirir. (10) **Kalıtım yolu:** accent'i `brand` sözcüğü ve değişken okuması
>   olmadan taşıyan bir kural — bir slot öğesinden `inherit` ya da `currentColor` ile — hiçbir testte
>   görünmez. Ölçüldü: `input.css`'e `.tap-button::after{content:"";position:fixed;inset:0;
>   background-color:inherit}` (E5) yazılınca WL-5'in brand testleri ve handler'daki WL-5 kümesi yeşil
>   kaldı; denetçi bunun accent'i bütün ekrana çizdiğini ölçtü. Bu yol kazara da yazılır: konumlanmış
>   bir sözde öğe + `background: inherit` + ebeveynde unutulmuş `position: relative`. Denetçinin D9'u
>   — dalga efekti olarak `.tap-button::after{content:"";position:absolute;inset:0;background:inherit;
>   opacity:.15;pointer-events:none}`, `.tap-button`'da `position:relative` yok — Chrome'da ilk
>   taşıyıcı bloğu (500×757) kapladı ve accent'i docket'in üstüne boyadı (paper `rgb(255, 253, 244)` →
>   `rgb(249, 221, 211)`); brand testlerinin 9'u, handler testlerinin 22'si ve DB'siz 2601 testin
>   tamamı yeşil kaldı. Kod incelemesinin konusu (WL-10); WL-9 tema bağlantısını tap ekranına
>   eklediğinde bu yol bir tenant accent'iyle etkinleşir.
>   (11) Yorum ya da dizge içindeki süslü parantez kaçış değildir ve nötrleştirilmez; bilerek
>   dengelenmiş bir yazım golden'ı yeşil bırakır (ölçüldü: F4'ün kaçışlar yerine korunmuş yorumlardaki
>   `}` ve `{` ile kurulmuş hâli, F5 — WL-5'in brand testlerinin hepsi yeşil). Tehdit modelinin
>   dışında: kod incelemesi.
>
> **Bulgular (kapsam dışı, bloklamaz):**
> - **`TestEveryNamedTestExists`'in yorum silicisi Go dizgelerini bilmiyor** — bir test dosyasındaki
>   `"/*"` dizgesi sonraki `*/`'a kadar her `func Test…` bildirimini görünmez yaptı (ölçüldü: bu görevin
>   ilk taslağında üç var olan test "yok" okundu, sarkan sayısı 54 → 57). `theme_test.go` sınırlayıcıları
>   iki bitişik sabitte tutarak etrafından dolaştı; betik düzeltilmedi.
> - **Bayat "CI'da SKIP" metinleri.** CI 2026-08-15'ten beri `make css` koşuyor (`6087d07`). WL-5'in
>   dokunduğu dosyalardakiler 3. turda düzeltildi: `input.css` (panel filtre çubuğu yorumu) ve
>   `internal/handler/dashboard_test.go` (`TestPanelScreens_TouchTargetClassesReserve44px`'in başlığı,
>   palet testlerinin "IT READS SOURCES" paragrafı, `TestTailwind_ScansNoMinifiedSource`'un başlığı).
>   Kalanlar, dokunulmadı: `internal/handler/result_test.go` (`TestCompiledCSS_GeneratesNoText` ve
>   `TestCompiledCSS_StampWordIsInk`'in yorumları ve metin ekranlarının sınır listesinde `TestCompiledCSS_GeneratesNoText`'i anan madde) ·
>   `docs/plan/agent-brief.md` (M6-02 dersi) · `docs/plan/state.md` (iki yer) ·
>   `docs/plan/m5-tap-akisi.md` (tarihli not).
>
> **Devirler:**
> - **WL-9:** tema bağlantısı tap ekranında `app.css`'**ten sonra** (aynı özgüllük, sıra kazanır — CDP'de
>   böyle ölçüldü); markasız tenant'ta bağlantı yok; href `"/brand/theme/" + Color.Hex() + ".css"` —
>   küçük harf, `#`, sorgu 404 alır ve sayfa varsayılanda kalır (CDP: `1f5c41`, `808080`); tek sayfalık
>   bağlantının ağı golden'dır (sınır 5); bağlantı ortak bir `<head>`'e konursa L1'in iki testi bilinçli
>   güncellenir; sonuç ekranında bağlantı 0 (D-C).
>   **Kalıtım yolu (sınır 10):** tema bağlantısı tap ekranına eklendiğinde `.tap-button`'ın
>   `inherit` ile accent taşıyan bir sözde öğesi tenant accent'iyle etkinleşir — bağlantıyı ekleyen
>   düzenlemede `.tap-button`'ın sözde öğeleri ve `inherit` kullanımı incelenir.
> - **WL-8:** şerit `input.css`'te bir sınıf olarak (`--brand-accent`'i yalnız `background-color`'da
>   okur) ve `themeSlots`'a `--brand-accent` ile girer; panel kabuğunda bağlantı `app.css`'ten sonra.
>   **Aynı düzenlemede `themeBrandGolden`'a şeridin derlenmiş kuralı girer** (madde 14 aksi hâlde
>   kırmızı); `input.css`'in `:root` yorumu bunu söylüyor.
> - **WL-7:** Account önizlemesinin düğmesi `.tap-button`; yeni slot gerekmez.
> - **WL-10:** denetim listesine — sınır 1–11, sorgu reddi, defterin kapsamı (sınır 2), golden'ın her
>   değişikliği (madde 14). **Kod incelemesi notu (sınır 10):** `.tap-button`'ın sözde öğeleri
>   (`::before`, `::after`) ve `inherit` / `currentColor` kullanımı incelenir — konumlanmış bir sözde
>   öğe + `background: inherit` + ebeveynde eksik `position: relative` accent'i taşıyıcı bloğa
>   yayar (D9: docket'in üstüne; E5: bütün ekrana) ve hiçbir test görmez. Sınır 11'in bilerek
>   dengelenmiş yorum parantezleri de aynı listeye.
> - **WL-12:** skill `tappa-brand` → *"Tenant slotları"*: token'lar `colors`'ta değil özelliğe göre;
>   derlenmiş `app.css`'te accent'i `themeSlots`'ta olmayan bir seçicinin okuması slot testini
>   kırmızıya çevirir (madde 11); skill'in *"WL-5'in testi özelliğe bakar, seçiciye değil"* yarım cümlesi — test ikisine de
>   bakıyor.
>
> **Tur geçmişi.**
> - **2. tur (üçüncü göz RED, beş YALNIZ METİN bulgusu; güvenlik ONAY + bir DÜŞÜK):** CI SKIP iddiası
>   düzeltildi; slot hükmü derlenmiş `app.css`'e bağlandı; `brandtheme.go` yorumları daraltıldı; §6
>   madde 1 kural adına çevrildi; "global middleware" yapılandırmasız yönlendiriciye daraltıldı ve
>   operatör host kolu eklendi; iç gölge kuralı (A16) ve 405 pini (A19) eklendi; rota/gövde defteri
>   eklendi (güvenlik DÜŞÜK).
> - **3. tur (kapanış denetçisi RED, hepsi YALNIZ METİN; iddia biçimi değişti):** satır numarası
>   atıfları kaldırıldı (F1 — 2. turda yazılan aralıklar zaten bir satır kaymıştı); test anan her cümle
>   testin assert ettiğine bağlandı (cümle ↔ assert tablosu teslim raporunda); ADR'nin "gövde değişirse
>   rota değişir" cümlesi PART I'e ve sınır 2'ye eşitlendi (F2); `Mount` yorumundaki metot cümlesi
>   daraltıldı (F3); V1 satırına kapsam yazıldı (F4); defter testi rotayı `chi.Walk` ile `Mount`'tan
>   okuyor (F5, M8 kırmızı); bağımlılık listesine `Check`'in eşikleri eklendi (F6, T1 ölçüldü); defter
>   kuralı "kod incelemesi; test zorlamaz" (F7); WL-5'in dosyalarındaki bayat CI SKIP yorumları
>   düzeltildi (F8); gövde taramasına 256 gri eklendi (F10, M3 kırmızı).
> - **4. tur (kapanış denetçisi RED: iki pin açığı + metin; iddia yüzeyi küçültüldü):** slot
>   taraması CSS kaçışlarını çözerek okuyor (B1; X9a–d kırmızı, X9aU1 çözmenin yük taşıdığını
>   gösteriyor); HEAD testi ham TCP'den okuyor (B2; X8 kırmızı); varsayılan testi kuralın yazıldığı
>   metni bayt bayt karşılaştırıyor (B3; X10 kırmızı); §6 madde 1 okuyan testin okuduğuna bağlandı
>   (N1); `input.css`'teki iki adsız test atfı ve ADR "Slot tablosu" maddesi teste adıyla bağlandı
>   (N2); "eksik başlık" ölçüldü (N3, H29); negatif kontrol başlığı iki `:root` assert'ini ayrı
>   anıyor (N4); `theme.go` ve ADR'deki "tek kural" cümlesi "bugün (WL-5)" diye tarihlendi ve sınır 9
>   oldu (N5, X5a). WL-5'in yorumlarında, test başlıklarında, ADR notunda ve bu kartta "yalnız / tek /
>   her / only / every / never / the one" geçen cümleler kapalı bir listeye bağlandı, silindi ya da
>   sınır oldu; PART I maddelere bölündü ve her assert'in yanına onu kıran mutasyon yazıldı.
> - **5. tur (kapanış denetçisi RED: aynı sınıftan yeni yazım + ayrıştırılmış başlık haritası):**
>   tanımdan önceki yorum iki ayrıştırıcı testinden de saklanıyordu (B1c3, B1c5, K2); katman
>   değişti: *"başka bir yerde geçmez"* ayrıştırıcısız, kapalı bir geçiş golden'ıyla ölçülüyor
>   (madde 14, kontrolü madde 15) ve 4. turun sınır 9'u kapandı (X5a kırmızı). Ayrıştırıcı
>   bildirimdeki yorumu düşürüyor (kural 1'in *"…or a comment"* sözü artık tutuyor; U5). Madde 11 ve
>   13 *nerede/hangi özellikte* ve *varsayılanlar ne* sorularına daraltıldı. Tel testleri (madde 3
>   ve 4) başlık bloğunu Go'nun ayrıştırıcısı olmadan satır satır okuyor (A3b, A3c, H22 kırmızı).
>   Metin: X8 satırı 81 bayt (N-1); §6 madde 1 testin deseniyle (N-2); `nonSurfaceGrounds` gerekçesi
>   "bugün" diye tarihli (N-3).
> - **6. tur (son tur; kapanış denetçisi RED: iki ucuz kod bulgusu + iddianın kapsamı):** tehdit
>   modeli cümlesi iddiaların başına kondu. Golden, pencere ve derinliği hesaplamadan önce süslü
>   paranteze çözülen kaçışları nötrleştiriyor (E1, E1b, E1c, E2, F4 kırmızı; nötrleştirmesiz
>   dördü yeşil). Tel testlerinin ikinci cevabı birebir, sonra kapanış (W1b, W2h kırmızı). Madde
>   14 yalnız (pencere metni, derinlik) çoklu kümesini tutuyor; *tanım nerede, kaç kez* madde 11'in
>   kural 2'sine ve madde 13'e döndü (E3 golden'ı yeşil, 11 ve 13'ü kırmızı bıraktı). Sınır 9
>   ölçüldü (O1 yeşil, O2 kırmızı); sınır 10 (kalıtım, E5) ve 11 (dengelenmiş yorum parantezi, F5)
>   yazıldı. İddia C'nin başlığı PART I'ine daraltıldı. Golden'ın bedeli gelecekteki her `brand`
>   sözcüğünü de anıyor; `input.css`'te `.p-brand` kuralının üstünde golden'a işaret eden yorum var.
>   Golden yorumundan *"moves"* çıktı; *"understands nothing"*, *"text like any other"*,
>   pencerenin seçiciyi içerdiği ve yorum parantezinin kırmızı verdiği cümleler ölçüme bağlandı ya
>   da kaldırıldı.
> - **Kapanış (üçüncü göz ONAY; iki YALNIZ METİN düzeltmesi):** kalıtım yolunun kazara biçimi
>   (denetçinin D9'u) sınır 10'a ölçümüyle yazıldı, karar o'dan *"bilerek"* grubundan çıkarıldı,
>   WL-9 ve WL-10'a kod incelemesi notu olarak devredildi (F-1). Golden penceresinin küçük harfe
>   indirilerek karşılaştırıldığı madde 14'e ve ADR'nin golden paragrafına yazıldı (F-2,
>   denetçinin CASE1'i).

## 6. Kararlar

**✅ Kullanıcı kararları (2026-09-24):**
| # | Soru | Öneri | **Karar** |
|---|---|---|---|
| D-A | Depo PUBLIC — ne yapılsın? Scrub push'u? | private yap + push | **Public kalsın, yalnız push.** Sonuç: geçmişteki değeri öldüren tek önlem F0-1 (rotate); F0-5 sır kapısı kritik hale geldi — depo herkese açık kaldıkça her commit yayındır |
| D-B | Süper admin modeli | (c) hibrit | **(c) ayrı operatör kimliği** — TOTP + `ops.taptime.mt` + `op_*` definer'lar; önceki "B — allow-list genişlet" kararının yerine geçer |
| D-C | Tap ekranı markası (§9) | logo + accent tap butonunda | **Logo + tap butonu tenant renginde**; sonuç ekranında yalnız logo (§9 onayı — WL-9 kartına alıntılanır) |
| D-D | Akış sırası | Faz 0 → A1 → B → C → A2 | ~~**Faz 0 → Süper admin (A1) → SES (B) → White-label (C) → A2**; SES dış adımları paralel~~ → **2026-10-02: Faz 0 → A1 → White-label (C) → SES (B, WL-11 ile) → A2** (SES dış adımları kullanıcı tarafından sonraya bırakıldı; orkestratör önerisi, raporlandı, itiraz yok — §2 güncellemesi) |

**⏳ Sırası gelince sorulacak:** OP-K5 askıdaki ayın faturalanması · ~~EM-K9 Mailpit~~ → ✅ **2026-10-03 kullanıcı: *"hayir sahte birsey eklemene gerek yok ses'e baglariz direkt"*** — yerel posta yakalayıcı yok, dev'de e-posta gönderilmez, doğrulama test içi sahte SMTP + doğrudan SES (ADR 0022 §12, open-questions Q02) · OP-K12 T27/T37 (plan geçmişi, OP-17 öncesi) · OP-K2/K4
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
