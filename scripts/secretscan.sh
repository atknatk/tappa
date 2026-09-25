# shellcheck shell=bash
# Tappa — R7d sir sizintisi desenleri. SOURCE EDILIR; dogrudan calistirilirsa yalniz
# `--hash <dosya>:<satir>` yardimcisi (muafiyet girdisi uretir; tablonun basligi).
#
# IKI TUKETICI, TEK TANIM (M10 F0-5, 2026-09-25):
#   scripts/redline-check.sh  R7d ......... commit'lenebilir AGACIN tamami
#                                          (git ls-files --cached --others
#                                          --exclude-standard: .gitignore'daki .env
#                                          taranmaz ve TARANMAMALIDIR)
#   scripts/git-hooks/pre-push .......... push edilecek araligin EKLENEN satirlari
#                                          + commit mesajlari + annotated tag mesajlari
# Tetikleyiciler, sinif desenleri, kayit bicimi ve muafiyet tablosu YALNIZ BURADA
# yazili. Iki kopya olsaydi biri digerinden sapar ve kanca agacin affetmedigi bir
# seyi affederdi (R1'in "tetikleyiciler tek yerde" dersi).
#
# NEDEN VAR (Olay A-0, docs/plan/m10-platform.md §0): 25. oturumda operator panel
# parolasinin DEGERI bir "rotate edilmeli" borc notunun ICINDE docs/plan/state.md'ye
# yazildi ve pushlandi. Depo PUBLIC — her push bir yayindir. Mevcut R kurallarinin
# HICBIRI docs/'a bakmiyordu (SRC yalniz kaynak kod), yani ag o dosyanin ustunde
# tamamen kapaliydi. R7d kapsami bu yuzden SRC DEGIL, commit'lenebilir her dosya.
#
# 🔴 BULGU METNI ASLA BASILMAZ. Cikti yalniz `yol:satir: [sinif]`. Bir sir
# tarayicisinin eslesen satiri ekrana basmasi, sirri CI log'una (public depoda
# public) ve sohbete ikinci kez yayinlamaktir — A-0'in kendisi. Satiri gormek
# isteyen onu yerel dosyada acar.
#
# KAYIT BICIMI (girdi): `yol US satir US metin`, US = 0x1F (R7D_SEP). `:` DEGIL,
# 2. tur denetimi olctu: `docs/plan/a:b.md` ve `docs/plan/notes:12.md` gibi yolu `:`
# tasiyan dosyalar `yol:satir:` ayristirmasinda SESSIZCE atlaniyordu (exit 0, kancada
# da). rg bunu `--field-match-separator` ile basar, kanca kendi uretir. Ayrisamayan
# her satir artik HATADIR (exit 2); tek sessiz istisna rg'nin ikili dosya bildirimi.
#
# SINIFLAR (her biri asagidaki awk programinda bir fonksiyon):
#   url-cred    scheme://kullanici:<parola>@host — parola bos/yer tutucu degilse
#   aws-key     AKIA|ASIA + 16 [0-9A-Z], tam 20 karakter, solunda harf/rakam yok
#   pem-key     -----BEGIN ... PRIVATE KEY----- (PGP "KEY BLOCK" dahil)
#   bcrypt      $2[abxy]$NN$ + TAM 53 karakter govde (22 tuz + 31 ozet). 53'ten
#               uzun/kisa bir kosu gecerli bir bcrypt ozeti degildir (olculdu:
#               agactaki iki `FAKEfake...` sabiti 55 karakter ve eslesmez).
#   known-token saglayici onekleri (2. tur): ghp_/gho_/ghu_/ghs_/ghr_ (+36),
#               github_pat_ (+22), sk_live_/rk_live_ (+20), xoxb-/xoxp-/xoxa- (+10),
#               glpat- (+20), AIza (+35), JWT (eyJ<10+>.eyJ<10+>.). Govdesinde 4'ten
#               az farkli karakter olan (ghp_xxxx...) dolgudur.
#   pw-assign   ANAHTAR=DEGER (bosluksuz `=`): anahtar password/passwd/passphrase
#               ile BITER (PGPASSWORD=x) YA DA son `_`/`-` parcasi pass/pwd/apikey/
#               api_key/secret_key/access_key/auth_token'dir (db_pass=x; bypass=x
#               DEGIL) · SQL `PASSWORD 'x'` ve `IDENTIFIED BY 'x'` · `curl -u
#               <kullanici>:<parola>` / `--user` · YAML `anahtar: x` (.yml/.yaml,
#               satir basi, ayni anahtar kumesi, `-`/`_` ayracli) · .md/.toml/.ini/
#               .cfg/.conf/.properties'te `anahtar: x` ve `anahtar = x`.
#               🔴 4. TURDAN BERI HER BICIMDE DEGERIN GUCUNE BAKILIR (assignval):
#               >=6 karakter, >=2 sinif, yer tutucu ve BASVURU degil (cagri, secici,
#               ENV adi, yol, URL); >=32 hex ya da uzun harf+rakam her zaman guclu.
#               Deger DILIN kuraliyla okunur: kabuk kelimesi bitisik tirnakli
#               parcalari ve `\x`'i birlestirir, YAML duz skaleri satir sonuna (ya da
#               ` #` yorumuna) kadardir, SQL'de iki tek tirnak bir tirnaktir. .md'nin
#               TIRNAKSIZ degeri duz yazi olabilir: orada kod bicimi de dislanir
#               (strongval; "password: required"). Deger `<x>`/`{{x}}`/`…`/`***`/
#               REPLACE_*/*PASSWORD* ise yer tutucudur. `$`/`%` ile baslayan deger
#               (6. tur, olculdu): `$` i ACAN baglamda (kabuk, Compose/CI YAML,
#               Makefile, Dockerfile, .env) `$X`/`${X}`/`$(..)`/`$1` basvurudur ve
#               `$AD` in ardindaki kuyruk >=8 karakter, >=3 sinif, kod bicimi disi
#               ise DEGERDIR; DUZ baglamda (.md, commit/tag mesaji, kaynak kod, URL
#               parolasi) yalniz `$AD`, `${...}` ve printf bicimi (`%s`, `%(ad)s`)
#               basvurudur, baska her sey `$` DAHIL tam deger olarak olculur. Kabugun
#               tek tirnaginda ve .sql dizgesinde `$` hic acilmaz. Tirnakli deger
#               basvuru (secici, ENV adi, yol) sayilmaz.
#   a0-token    OLAY A-0'IN SEKLI: satirda bir sir kelimesi (R7D_TRIGGERS) VE ayni
#               satirda tirnak/backtick icinde >=8 karakterli, bosluksuz, en az UC
#               karakter sinifli (kucuk · buyuk · rakam · ASCII sembol) bir belirtec
#               — kod bicimi degilse (asagidaki `codeish` listesi, her maddesi agacta
#               olculmus bir yanlis pozitifin adidir). Alt sekil: >=32 haneli HEX
#               (kartin "uzun hex parola bicimleri"; iki sinifli oldugu icin ayri).
#               11. turdan beri ANAHTAR baglami (R7D_KEYCTX: kek, hmac, aes, key,
#               anahtar) da tetiktir, ama YALNIZ anahtar bicimli aday icin (keyshaped).
#
# --- OLCULEN (2026-09-25, HEAD 573d4e1, 617 izlenen dosya) --------------------
#   sinif       ham isabet                      desen ayarindan sonra   muaf   FAIL
#   url-cred    14 (kullanici:parola@)          8 (6'si yer tutucu)      8      0
#   aws-key     0                               0                        0      0
#   pem-key     0                               0                        0      0
#   bcrypt      10 govde>=53 (86 onek animi)    8 (iki FAKE 55 karakter) 8      0
#   known-token 0 (2. tur, her onek ayri)       0                        0      0
#   pw-assign   37 (30 `=` + 4 SQL + 3 YAML) +  7 (var/yer tutucu/bos)   7      0
#               2. tur: yeni anahtarlar `=` 0,
#               md/toml 28 aday (hepsi yer
#               tutucu/var/bos), IDENTIFIED 0,
#               curl -u 0
#   a0-token    362 parca (103'u tek satirlik   6 satir                  6      0
#               vendored htmx'te) + hex alt
#               sekli: 2 aday, ikisi de dolgu   0
#               (AAAA..., 0123456789abcdef..)
#   Toplam: 29 satir, 29'u tabloda ADIYLA muaf, 0 FAIL. a0-token'daki 362 -> 6
#   dususu `codeish` ve `secretish` icindeki sekillerle (her birinin yaninda onu
#   doguran gercek ornek yazili). Olay A-0'in kendisi: pre-scrub state.md (a57d6e7) bu
#   kuralda 4 satir FAIL verir — A-0 kaydindaki "4 satir" — ve 920baaa'da 0; satir
#   sayisi 9edad5e/5cca991/b348b8b'de 2 -> 3 -> 4 buyur ("3 commit"). Yalniz SAYILAR
#   olculdu; eslesen metin hicbir yere basilmadi.
#   Tam gecmis, kancayla (275 commit, "yeni uzak" senaryosu): muaf olmayan
#   isabetlerin TAMAMI A-0 penceresindeki 9 eklenen state.md satiri. Yol boyunca
#   bulunan gecmis yanlis pozitifler (bir commit mesajindaki dev kimligi, bir backlog
#   satirindaki `2>/dev/null`, `--text` ile okunan bir woff2'nin rastgele baytlari)
#   tablo, `codeish` ve kancanin NUL kurali ile kapandi.
# --- 2. TUR (2026-09-25, ucuncu goz RED: B1 B2 N1-N6) --------------------------
#   Agac (620 dosya, yeni 3 dosya dahil): hala 0 FAIL; muaf 29 satir (ayni kume,
#   SINIRLI eslesmeyle de tutuyor) + tablonun kendi 16 satiri. 2. turun yeni
#   siniflari/anahtarlari agacta 0 ham isabet verdi; md/toml atama kuralinin 28
#   adayinin hepsi yer tutucu/degisken/bos. Tam gecmis kanca taramasi 7 sn, sonuc
#   degismedi (9 A-0 satiri; muaf 31, fd56c28 mesaji dahil). a57d6e7 hala 4.
#   Ayni R7d ciktisi macOS (BSD awk, rg 15.2) ile ubuntu:24.04 (mawk 1.3.4
#   20240123, rg 14.1) uzerinde birebir ayni (olculdu, Docker).
# --- 3. TUR (2026-09-25, ucuncu goz RED: B1 kalintisi + 7 ucuz bulgu) -----------
#   Muafiyet belirtecleri TURLU yapildi (@yaml/@sh/@sql); 4. tur bunu da KIRDI (satir
#   sonunda bitmeyen deger) ve tur makinesi 4. turda SILINDI — asagida.
#   Yeni: >=32 harf+rakam alt-sekli (SES SMTP parolasi bicimi) — agacta sir kelimeli
#   satirda 7 aday: 5'i dolgu (FAKEfake..., periyodik, desenle), 2'si sentetik test
#   degeri (16 satir, tabloda adiyla). Agac: 0 FAIL; WARN 61 = 45 muaf + tablonun 16
#   satiri; tablo 24 satir, 32 belirtec. Tam gecmis kanca taramasi (275 commit, 7 sn):
#   muaf olmayan isabet yine YALNIZ A-0 penceresinin 9 state.md satiri; muaf 47.
# --- 4. TUR (2026-09-25, ucuncu goz RED: satir sonunda bitmeyen deger) ----------
#   pw-assign'in BUTUN alt kurallari degerin gucune bakiyor (assignval: >=6 karakter,
#   >=2 sinif, yer tutucu ve BASVURU degil; hex/uzun harf+rakam her zaman). Olculdu:
#   agacta, muafiyet tablosu BOSKEN bile 0 pw-assign isabeti — dev degeri satirlari
#   (5 dosya) ve fd56c28 mesaj satiri gereksizlesti ve SILINDI; @yaml/@sh/@sql tur
#   makinesi de. Kalan tablo 18 satir, 24 belirtec; 38 muaf satirin 38'i en siki
#   varsayilan sinirla (bosluk/`+` degil) da muaf (liste 3. turla birebir).
#   Agac: 0 FAIL; WARN 49 = 38 muaf + tablonun 11 satiri. Tam gecmis kanca taramasi
#   (275 commit, 7 sn): muaf olmayan isabet yine YALNIZ A-0'in 9 state.md satiri; muaf
#   38 (dev degeri satirlari artik hic yakalanmiyor). a57d6e7 hala 4. Onceki turlarin
#   kesin-kirmizi vakalari (A-0, state.md'de tek harfli-parolali DSN, guclu `DB_PASS`,
#   dev degerinin guclu uzatmalari: bitisik, YAML'da sekiz ayrac, kabuk tirnagi/`\`,
#   SQL `''`) hala kirmizi; sessiz kalanlar yalniz zayif (`tappax`) ve satirlar arasi
#   devam (YAML cok satirli skaler, SQL bitisik dizge) — sayili sinirlar #2 ve #6.
# --- 5. TUR (2026-09-25, ucuncu goz RED: @prefix ikinci degeri affediyordu) ------
#   `@prefix=` turu ve tek satiri (rotatekek testi) SILINDI: belirtec acilis tirnagini
#   da cikariyordu, satirin tirnak paritesi kayiyor ve ayni satira eklenen ikinci
#   tirnakli deger uc sekilde sessiz kaliyordu (olculdu). Test degeri artik calisma
#   aninda 8 karakterden kisa parcalardan kuruluyor (bayt bayt ayni). Tirnak tasiyan
#   belirtec artik tabloyu exit 2 ile reddeder. Deger okuyuculari: YAML tirnakli
#   skalerin kacislari, dugum ozellikleri (&cipa, !etiket), tirnakli degerde refish
#   YOK, `$`/`%` + guclu govde DEGER (N1-N4). Agac: 0 FAIL; WARN 48 = 37 muaf +
#   tablonun 11 satiri; tablo 17 satir, 23 belirtec. Muafiyet tablosu BOSKEN agacin
#   FAIL listesi 4. tur siniflandiricisiyla SATIR SATIR ayni (37 satir): 5. turun
#   deger okuyuculari agacta yeni yanlis pozitif getirmedi. Gecmiste getirdi: `$` +
#   govde kuralinin ilk hali deploy.yml gecmisindeki (fd56c28..) bir `"$HOME/..."`
#   yolunu yakaladi; `$AD`'in ARDINDAKI kalan kisma bakilarak kapandi (sigilref).
#   Tam gecmis kanca taramasinda A-0'in 9 satirina rotatekek testinin a32d0ca'daki
#   TEK satiri eklendi (artik muaf degil; yalniz tum gecmisi tarayan URL/yeni-uzak
#   push'unda gorunur, o da A-0 yuzunden zaten exit 1'dir); muaf 37. a57d6e7 hala 4.
# --- 6. TUR (2026-09-25, ucuncu goz RED: belirtecteki sir kelimesi) -------------
#   Muafiyet belirteci silinince ICINDEKI sir kelimesi de gidiyordu: ADR 0019'un
#   ornek parolasi (ADR, signup.go) ve CI-only sahte KEK (ci.yml) satirina eklenen
#   ikinci guclu tirnakli deger, sir kelimesi yalniz belirtecte oldugunda sessizdi
#   (denetci 13 sekilde olctu; kanca ve redline exit 0). MEKANIZMADA cozuldu: silme artik satir
#   UZUNLUGUNU korur, BAGLAM yuklemleri (sir kelimesi, atama anahtari, `curl`)
#   ORIJINAL satirdan, deger adaylari ayni konumdan silinmis satirdan okunur.
#   Diger siniflarin baglami degerin kendisidir (sema, onek, AKIA) — sinirli bir
#   belirtec onu tasiyamaz. Atama anahtari icin ayirt edici vaka YOK: sinirli bir
#   belirtecin ardinda satir sonu ya da tirnak+noktalama vardir, yani belirtecin
#   icindeki bir anahtarin degeri belirtecin disinda olamaz — ctx/t mutasyonu orada
#   hayatta kalir (olculdu, kurali tek bicimli tutmak icin ctx). `$`/`%` (B3): duz
#   baglamda `$` hicbir sey acmaz (plainpath). Kabuk kelimesi, anahtar bir cift
#   tirnakli dizgenin ICINDEYSE kapanan `"` da biter. Agac: 0 FAIL; WARN 48 = 37 muaf
#   + tablonun 11 satiri; tablo 17 satir, 23 belirtec. Muafiyet tablosu BOSKEN agacin
#   FAIL listesi 5. tur siniflandiricisiyla satir satir ayni. Tam gecmis kanca
#   taramasi: yine 9 A-0 + 1 rotatekek satiri, muaf 37; a57d6e7 hala 4. (Belirtec
#   silme, ctx aktarimi ve tablonun kendi satiri istisnasi 7. turda SILINDI.)
# --- 7. TUR (2026-09-25, ucuncu goz RED: sol uzatma; orkestrator: yeniden tasarim) ---
#   Muafiyet artik SATIRIN TAMAMINA bagli (yol + satirin sha256'si; tablonun basligi).
#   Satir-ici belirtec mekanizmasinin tamami silindi. Tablo 6. turun muaf ettigi 37
#   agac satirindan uretildi: 32 girdi (adminreset_test'te bayt bayt ayni satirlar tek
#   girdi); yeni mekanizmanin muaf listesi eskisiyle BIREBIR ayni (37). Tablonun kendi
#   satirlari istisnasiz temiz (olculdu). htmx satiri tek satirlik dosyanin tamami
#   oldugu icin girdinin hash'i README'deki sha256'nin KENDISIDIR; `@class` gerekmedi.
#   Ozellik testi (TestSecretScan_AWaiverIsBoundToTheWholeLine): muaf her satirin
#   basina, sonuna, her tirnak/ayrac komsulugunun iki yanina ve 10 rastgele konuma
#   guclu bir deger eklenir (htmx: 30 ornek komsuluk) — 1178 satirin 1178'i adsiz
#   yoldakiyle birebir ayni siniflanir, hicbiri muaf degil; ayni kume 6. turun belirtec
#   mekanizmasinda 792 satir SESSIZDI (olculdu). Agac: 0 FAIL, WARN 37. Muafiyet tablosu
#   BOSKEN agacin FAIL listesi 6. turla satir satir ayni ve tam olarak o 37 satir. Tam
#   gecmis kanca taramasi: 9 A-0 + 1 rotatekek, muaf 37; a57d6e7 hala 4. Ayrica: kabuk
#   kelimesinde kapanan `"` ardindan kelime/tirnak geliyorsa kelime surer (Bulgu 2);
#   deploy/k8s YAML'inda yalniz `$(AD)` acilir (Bulgu 3).
# --- 8. TUR (2026-09-25, ucuncu goz ONAY; bloklamayan bulgular) ----------------
#   Kanca olculen git ayarlarindan bagimsiz okur (pre-push basligi: dstPrefix /
#   noprefix / UTF-16 log kodlamasi olculdu; tamlik iddiasi yok). Tablo aciklamasi deger tasiyamaz (sir
#   kelimesi olmasa da); yol tek bir dosyayi adlandirmak zorunda; `--hash` yardimcisi
#   `\r` ve sondaki boslugu korur (testle pinli); redline R7d FAIL mesaji yardimciyi
#   gosterir; `KEY=%-20s` gibi bosluksuz printf bicimi kod sekli. Agac: 0 FAIL, WARN
#   37. Muafiyet tablosu BOSKEN agacin ve tam gecmisin FAIL listeleri 7. turla satir
#   satir ayni (37 ve 47). Tam gecmis: 9 A-0 + 1 rotatekek, muaf 37; a57d6e7 hala 4.
# --- 9. TUR (2026-09-25, ucuncu goz RED: 8. turun printf kurali) ----------------
#   8. turun printf kurali `ANAHTAR=` + harf/rakam + tek bir `%<harf>` tasiyan her adayi
#   kod sayiyordu: 7. turda kirmizi 13 deger sessizdi, rastgele 12 karakterlik degerde
#   yakalanan 248 -> 32 (denetci olctu). Kural artik YALNIZ printf fiilleri ve
#   alfanumerik OLMAYAN ayraclardan olusan degeri kod sayar; sabit tohumlu 2 x 300
#   ornekte kural acik ve kapaliyken ayni satirlar raporlanir
#   (TestSecretScan_ThePrintfRuleSilencesNoRealisticValue; kapsami sinir #27). Tablo aciklamasi bosluk-kelimeleriyle
#   de sinanir (N3); tablo hatasi satir metnini basmaz (N5); kanca baglam satirlarina ve
#   yanlis `encoding` basligina dayanikli (N2, N1). Agac: 0 FAIL, WARN 37.
# --- 10. TUR (2026-09-25, ucuncu goz ONAY; yalniz belge/test + mergetag) ----------
#   Sayildi: printf kuralinin susturdugu iki bicim (#27), 128 karakterden uzun a0 adayi
#   (#28); bilinen gurultuye Go `%[1]s` / `100%%s`. Kanca, imzali bir tag birlestirilince
#   merge commit'in `mergetag` basligina gomulen tag mesajini da okur (pre-push
#   raw_msg_lines). Ham mesaj gecisinin iki korumasi testle pinli
#   (TestPrePush_TheRawMessagePassIsPinned). Agac: 0 FAIL, WARN 37.
# --- 11. TUR (2026-09-25, tappa-security-auditor ONAY; ORTA §4.7) ---------------
#   KEK ve NTAG AES anahtarlari A-0 biciminde yazilinca sessizdi (denetci alti biciminin
#   altisini olctu): "parola" degil "KEK", "anahtar", "key" denir. R7D_KEYCTX bu
#   kelimeleri YALNIZ anahtar bicimli tirnakli bir degerle birlikte tetik sayar
#   (keyshaped); atama kurallarina `kek` ve `hmac_key` eklendi — ornek Secret'in yedi
#   adinin yedisi artik bir kurala giriyor (DSN'ler url-cred, parolalar pw-assign).
#   Muafiyet tablosu BOSKEN agacta 23 yeni satir cikti, hepsi siniflandirildi: CI'nin
#   belgelenmis sahte KEK'i (cozulup karsilastirildi), AN12196 bilinen-cevap vektorleri,
#   `_label: FAKE` fixture — gercek bir sizinti YOK. 18 satira bagli girdiyle muaf
#   (tablo 50 girdi, agacta 60 satir); `vcs.revision` git SHA-1'i (40 hex) desen
#   daraltmasiyla. Tam gecmis: yine 9 A-0 + 1 rotatekek, muaf 60. Ayrica: r7d_select
#   alt kabugu gecici dizinini EXIT/HUP/TERM tuzaklariyla siler, kanca SIGHUP'ta.
# --- 12. TUR (2026-09-25, SON sertlestirme; ucuncu goz 12. turda ONAY verdi) -----
#   `kek`/`hmac_key` adin ortasinda bir rotasyon sonekiyle de atama anahtari
#   (`TAPPA_TAG_KEK_PREVIOUS=`, `TAG_KEK_OLD:`; #32). camelCase bir adin icindeki
#   anahtar kelimesi (`tagKey`, `prodKEK`; R7D_KEYCAMEL) baglam. Muafiyet tablosu BOSKEN
#   agacta 14 yeni satir (hepsi test degeri: AN12196 vektorleri, belge disi bir bayt
#   rampasi, adinda fake/test/wrong gecen degerler, sun_vectors.json sahte degerlerinin
#   kopyalari — deger esitligi
#   olculdu; GERCEK SIZINTI YOK), 14 girdiyle muaf: tablo 64 girdi, agacta 74 satir.
#   Tam gecmis: 9 A-0 + 1 rotatekek, muaf 74. Sayildi: #31-#32 ve #30'a eklenen anahtar
#   bicimleri; bilinen gurultuye anahtar baglaminin bes bicimi.
#
# --- NEYI YAKALAMAZ (hepsi yanlis-NEGATIF; ag mekaniktir, KANIT DEGILDIR) -----
# Kural (agent-brief): yeni bir kacis kanali sonsuza kadar KAPATILMAZ, SAYILIR.
# Liste ag hakkinda bir tamlik iddiasi tasimaz. 2. tur denetcisi 72 sentetik sonda
# kostu, 30'u kacti; raporunda SINIFLANDIRDIGI kacislar ya asagida sayili ya da 2.
# turda kapatildi (baslikta). 72 sondanin kendisi bu turda yeniden kosulMADI.
#   1. TIRNAKSIZ DUZ YAZI: "şifre: <deger>", "the admin password is <deger>" — sir
#      kelimesi ile deger arasinda tirnak/backtick yoksa a0-token girmez. .md'de
#      `anahtar: deger` bicimi pw-assign'a girer, ama yalniz anahtar ADI
#      password/pass/pwd/api_key/... ile biterse ve deger >=6 karakter, >=2 sinifli,
#      kod bicimi disi ise (duz yazinin "password: required" cumlesi yuzunden). Ayni
#      guc sarti 3. turdan beri `=` atamasindaki YENI anahtarlar (pass/pwd/api_key...)
#      ve YAML'daki yeni anahtarlar icin de gecerli (`local pass=0` gurultusu).
#   2. ZAYIF deger: tek sinifli (`tappa`, `hunter`), 6 karakterden kisa, ya da yalniz
#      kucuk harf+rakam / yalniz hex ve a0-token'in 3 sinif esigi altinda (hex >=32
#      hane haric). 4. turdan beri atama bicimlerinde (`X=`, SQL, YAML, TOML, curl)
#      de ayni: dev degerleri bu yuzden HIC yakalanmaz — bilincli, muafiyetten
#      guvenli bir sinir.
#   3. TANIMLAYICI SEKLI parola: harf+rakamdan olusan, sembolsuz, 32 karakterden KISA
#      bir deger bir Go/ENV adiyla BAYT BAYT ayni bicimdedir; ayirmak bir ayristirici
#      ister (>=32 karakter + buyuk/kucuk/rakam 3. turda yakalanir: longalnum). `_` ya
#      da `-` tasiyan base64url deger (uzun da olsa) Go test adlariyla ayni bicimde
#      (olculdu: TestEV2_..., TestADR0005_...) ve YAKALANMAZ. Ayni sebeple
#      `Kelime.Kelime2026` gibi noktali bir deger pkg.Func sayilir. Tek bir ASCII sembol
#      (`!`, `#`, `$`...) tasiyan deger bu kapidan GECMEZ, yakalanir. .md/.toml
#      atamasindaki deger de ayni kod-bicimi suzgecinden gecer.
#   4. KEBAB SEKILLI deger: tumu kucuk harfli tireli kelime gruplari
#      (<kucuk>-<kucuk>-<rakam>) kod adlariyla (dal adlari, dosya adlari) ayni
#      bicimde; bir parca KARISIK harfliyse (<Buyuk><kucuk><rakam>-...) yakalanir.
#   5. K:V CIFTI a0-token icinde: `<tanimlayici>:<harf/rakam>` (denetcinin sondasi
#      `svc-ops:<deger>`) key=value/key:value kod sekli sayilir; deger `!#$%&@^~`
#      sembollerinden birini tasirsa yakalanir.
#   6. FARKLI SATIRLAR: sir kelimesi/anahtar ile deger (ya da degerin devami) ayri
#      satirlarda — YAML cok satirli duz skaler (bir alt satira daha iceriden yazilan
#      devam), SQL bitisik dizge (`'a'` ve alt satirda `'b'`), kabuk satir devami (`\`).
#      Satir yerel tarayici bunlari birlestirmez.
#   7. TETIK/ANAHTAR LISTESINDE OLMAYAN kelime: `pw`, `pin`, `kimlik bilgisi`... ne
#      a0-token'in sir kelimesi ne pw-assign'in anahtari. (`key`, `kek`, `hmac`, `aes`,
#      `anahtar` 11. turdan beri ANAHTAR baglamidir — yalniz anahtar BICIMLI tirnakli
#      bir degerle, #30; `kek` ve `hmac_key` ayrica atama anahtaridir, #32.)
#      `token=`/`secret=` bilincli olarak pw-assign'da YOK (URL sorgulari);
#      a0-token'da ise ikisi de tetik. Liste disi saglayici onekleri (Azure, npm,
#      PyPI, Twilio...) known-token'a girmez.
#   8. Ikili dosyalar: agacta rg ikili (NUL iceren) dosyayi atlar; kanca da BLOB'U
#      NUL tasiyan dosyayi atlar (3. tur: karar git nesnesinden, metindeki bir isaret
#      baytindan DEGIL). Anahtar dosyalari R7'nin `git ls-files '*.pem' ...`
#      kontrolunde.
#   9. url-cred parolasi `/` iceriyorsa (URL'de %-kodlanmamis) otorite erken biter.
#      curl -u yalniz ayni satirda `curl` kelimesi ve ayri bir `-u`/`--user` ile.
#      `mysql -p<parola>` (bitisik, anahtarsiz) ve `Authorization: Basic <base64>`
#      basligi HICBIR sinifa girmez (3. tur, sayili).
#  10. Kanca: `git push --no-verify` kancayi atlar; kancayi tasimayan eski bir commit
#      checkout edilmisse core.hooksPath bos bir dizini gosterir ve git push'u
#      SESSIZCE gecirir. CI'daki R7d ikinci agdir, ama public depoda o ag ancak
#      yayindan SONRA gorur. Kanca aralik icin uzagin ADINI kullanir: URL ile ya da
#      yeni bir uzaga ilk push TUM gecmisi tarar ve bu depoda Olay A-0 satirlari
#      yuzunden exit 1 verir (pre-push basligi, Makefile `hooks`). En fazla 8 ic ice
#      annotated tag okunur; 9. kat exit 2 (sessiz degil). `.git/info/grafts`
#      (kullanimdan kalkmis) GIT_NO_REPLACE_OBJECTS ile KAPANMAZ: kanca graft'li
#      tarihi okur ve degeri tasiyan commit'i gormez (olculdu, git 2.50: kanca 0
#      satir). Ama push da graft'i izler, o commit'i YOLLAMAZ ve uzak taraf
#      `missing necessary objects` ile reddeder — deger yayinlanmaz (olculdu).
#  11. MUAFIYETIN KENDISI: tablodaki bir (yol, sha256) o dosyada bayt bayt AYNI olan
#      HER satiri affeder — muaf bir satirin ayni dosyaya kopyalanmasi yeni bir deger
#      tasimaz, ama ayni degerin ikinci bir yerde durdugunu da gizler. Muaf satir
#      degisirse muafiyet duser (yanlis POZITIF yonu, bilincli). Muafiyet GORUNURDUR:
#      her kosuda WARN'da `yol:satir: [muaf]`.
#  12. Yolunda US (0x1F) ya da satir sonu tasiyan dosya: ayristirma HATA verir
#      (exit 2, sessiz degil). Kancada git'in tirnakladigi (`"`, `\`, kontrol
#      karakterli) yol kacisli bicimiyle raporlanir ve hicbir muafiyete uymaz.
#  13. base64 ALT BICIMLERI (4. tur N3): `=` dolgulu ama `+`/`/` tasimayan base64
#      (dolgu tek sembol, sinif sayimina girer ama codeish'in key=value sekline
#      takilabilir) ve `/` ile BASLAYAN base64 (yol sayilir).
#  14. YAPILANDIRMA BICIMLERI (N4): YAML akis bicimi `{DB_PASSWORD: <deger>}` (anahtar
#      satir basinda degil), pgpass satiri (`host:port:db:kullanici:parola`), SQL
#      `E'...'` kacisli dizgesi.
#  15. Kancada NUL tasiyan bir blob'a eklenip SONRAKI commit'te NUL'u kaldirilan
#      dosyanin o ilk hali taranmaz (ikili sayilir); CI agac taramasi yayindan SONRA
#      gorur (N6).
#  (16-23: 5. tur denetiminin N5 listesi; her biri SAYILDI, kapatilmadi. ORTAK
#  NOT, olculdu: bu bicimleri pw-assign OKUMAZ; ama deger TIRNAKLI, satirda bir sir
#  kelimesi var ve deger a0-token esigini (>=8 karakter, >=3 sinif) geciyorsa a0-token
#  yine yakalar — `U&'...'`, `E'...'`, SET PASSWORD =, TOML uc tirnak, tirnakli
#  --password / ENV / := / tek tirnakli YAML anahtari. KACAN: tirnaksiz deger, a0
#  esiginin altindaki deger (2 sinifli ya da 6-7 karakter) ve sir kelimesi olmayan
#  satir.)
#  16. KABUK: `${X:-<deger>}` / `${X:=<deger>}` varsayilan degeri (`${` basvurudur),
#      `--password <deger>` (BOSLUK ayracli), `sshpass -p <deger>` (tetik yok), `.sh`
#      heredoc icindeki YAML (yol .sh; YAML kurali yalniz .yml/.yaml yolunda).
#      `$'...'` ANSI-C dizgesi 5. turdan (N2) beri govdesi guclu ise YAKALANIR —
#      kacislar cozulmez ama tamamen `\xNN` ile yazilmis bir deger de uc sinif sayar
#      (olculdu); govdesi tanimlayici bicimindeyse basvuru sayilir ve kacar (#23).
#  17. Dockerfile `ENV K V` (bosluk ayracli); Makefile `K := V`, `K ?= V`, `K = V`
#      (bosluklu `=`; (1) bosluksuz `=` ister).
#  18. YAML: TEK tirnakli anahtar (`'password': x`), `.yml.tmpl` gibi uzantisi
#      farkli yol, ve ardinda deger olmayan tek bir ozellik (`password: !Zq9...`,
#      `password: &Zq9w!xLm3` — YAML'a gore etiket/cipa, deger null; olculdu).
#  19. SQL: `$$...$$` ve `$tag$...$tag$` dolar tirnagi (tirnak yok, a0 de gormez),
#      `U&'...'`, `E'...'` (#14), `SET PASSWORD ... = '...'` (`=` ayracli).
#  20. TOML cok satirli `"""..."""`; `.conf`/`.netrc` BOSLUK ayracli deger
#      (`password <deger>`, `machine h login u password <deger>`).
#  21. KOD bicimleri, sir kelimesi yoksa: JSON `"api_key": "<deger>"` (.json (5)'te
#      yok), Python `api_key = "<deger>"` (bosluklu `=`), Go `APIKey: "<deger>"`.
#      (`key`/`Key` bir sir kelimesi degil, bir ANAHTAR baglamidir: deger anahtar
#      BICIMLIYSE — >=32 hex, uzun harf+rakam, 32/16 bayt base64 — bu satirlar da
#      yakalanir, #30; kisa ya da parola bicimli bir deger kacar.)
#  22. .md/.ini/.toml TIRNAKSIZ deger `& ; , ( ) { } | \ < >` karakterlerinde ve
#      tirnakta kesilir (val_at): `password: Zq9w&xLm3` -> `Zq9w` (zayif).
#  23. `$` ACAN baglamda (kabuk tirnaksiz/cift tirnak, Compose/CI YAML — tek
#      tirnakli da, Compose orada da acar —, Makefile, Dockerfile, .env ve bu
#      dosyalardaki SQL dizgesi): `$` + TANIMLAYICI govde (`$Zq9wxLm3Kp7`), `$AD` +
#      KISA kuyruk (<8 karakter ya da <3 sinif: `$Kx9!pQ2w`), `${X}` + kuyruk ve
#      `$1` + kuyruk basvurudur ve KACAR (olculdu). Kabugun tek tirnaginda ve .sql
#      dizgesinde `$` acilmaz, deger oradadir. DUZ baglamda kacan: `$AD` + yol
#      bicimi (`$Kx9/pQ2w` — `$HOME/.kube/config` kod sekli sayilir) ve `${...}`
#      ile biten bir deger. GitHub Actions YAML'inin `env:` degerlerinde `$X`
#      harfi harfine degerdir ama dosyanin tamami (run: bloklari kabuktur) acan
#      baglam sayilir: orada da yukaridaki kisa kuyruk kacar (7. tur, sayildi).
#      deploy/k8s YAML'inda yalniz `$(AD)` basvurudur, `$X` ve `${X}` degerdir.
#  24. BOSLUKLU a0 ADAYI (8. tur, sayildi): ayni tirnak/backtick icinde degerden sonra
#      bosluk ve kelime gelirse (`<deger> rotate edilmeli`, tek bir backtick araliginda)
#      a0-token susar — secretish bosluklu adayi reddeder (duz yazi cumleleri yuzunden).
#      Adsiz yolda da ayni; muafiyetle ilgisi yok.
#  25. YALAN SOYLEYEN ARACLAR (8. tur, sayildi): kapali-basarisizlik yalniz COKEN ya da
#      eksik araci kapsar (rg, awk, sha256, git, mktemp: exit 2). Bilinen vektoru
#      gecip baska girdide yanlis hash donen bir sha256 araci, girdiyi yutup 0 donen
#      bir awk ya da ciktisini degistiren bir git sarmalayicisi yakalanmaz.
#  26. MUAFIYET YOLU KISITI (9. tur, belgelendi): tablo yolu yalniz harf, rakam, `_`,
#      `-`, `/`, `@` ve `[.]` tasiyabilir; `--hash` yardimcisi da ayni kumeyi ister.
#      Adinda bosluk, `+` ya da Unicode olan bir dosyaya muafiyet YAZILAMAZ (agacta
#      boyle dosya 0). Gerekirse dosya yeniden adlandirilir.
#  27. PRINTF KURALININ SUSTURDUGU IKI BICIM (10. tur, sayildi): `ANAHTAR=`/`ANAHTAR:`
#      ardinda (a) ISIMLI bir fiilin icindeki tanimlayici — `%(<32 harf+rakam>)s`
#      (`API_KEY=`, `svc:`, Go `"JWT_SECRET=..."`), (b) YALNIZ fiillerden olusan bir
#      deger — `%a%O%B%N`, `%9876543210d`, `%-8.5x@%+3d`. Denetcinin simulasyonu:
#      gercekci alfabelerde 240 000 degerde fark 0; fark yalniz %50 `%` agirlikli bir
#      alfabede (5 000'de 1). Test (ThePrintfRuleSilencesNoRealisticValue) yalniz kendi
#      tohumlu orneklemi icin konusur.
#  28. 128 KARAKTERDEN UZUN a0 ADAYI (10. tur, sayildi): secretish 128'den uzun adayi
#      atar (uzun kod/URL/base64 gurultusu). Sir kelimeli satirda backtick icinde 160
#      haneli hex, ya da 125 karakterlik bir kosuya yapisik 8 karakterlik guclu bir
#      deger a0-token'da sessizdir (olculdu).
#  29. COMMIT BASLIK ALANLARI (11. tur, sayildi): kanca commit'in author/committer ve
#      tag'in tagger alanlarini, `mergetag` DISINDAKI ozel basliklari ve ref adlarini
#      taramaz. Yazar adina A-0 bicimli bir deger yazilan commit'te kanca exit 0 verir;
#      ham nesnede deger author/committer satirlarinda durur (denetci olctu).
#  30. ANAHTAR BAGLAMI (11. tur): R7D_KEYCTX yalniz TIRNAKLI ve anahtar BICIMLI adaya
#      bakar (>=32 hex, >=32 harf+rakam, 32/16 bayta cozulen base64). Tam 40 haneli hex
#      (git SHA-1 bicimi) anahtar sayilmaz; tirnaksiz bir anahtar degeri ancak atama
#      kurallarinin anahtarlariyla (kek, hmac_key, ...) yakalanir. 12. tur: camelCase
#      bir adin icindeki kelime (`tagKey`, `prodKEK`, `sessionHMACKey`; R7D_KEYCAMEL) de
#      baglamdir. Bu kural su anahtar bicimlerini TANIMAZ (12. tur, sayildi): 16+16
#      hane olarak bolunmus hex, `0x` + kucuk harf hex, `:` ile ayrilmis hex bayt
#      dizisi, backtick icinde degerden hemen sonra gelen nokta, 64 baytlik (88
#      karakter) base64 HMAC anahtari. Tirnaksiz markdown degerinde `=` dolgulu ve
#      `+`/`/` tasimayan base64 kod sekli (`ad=`) sayilir — #13.
#  31. SINYAL YARISI (12. tur, olculdu): kanca SIGHUP ile taramanin ortasinda
#      kesilince siniflandiricinin gecici dizini — butun tuzaklar yerindeyken — Ubuntu
#      bash 5.2'de 250 kosunun 1'inde kaldi. Kancanin ust duzey HUP tuzagi o kalintiyi
#      12/20 -> 0/20'ye indiriyor (macOS bash 3.2'de tuzaksiz da 0/20); testteki tek
#      kosu onu yalniz OLASILIKLA pinler (~%60).
#  32. ROTASYON SONEKLERI (12. tur): `kek`/`hmac_key` adin ortasinda yalniz bir
#      rotasyon sonekiyle taninir (`_previous`, `_prev`, `_old`, `_new`, `_next`,
#      `_current`, `_b64`, `_base64`, `_hex`, `_raw`, `_vN`/`_N`): `TAPPA_TAG_KEK_PREVIOUS`
#      evet, `kek_rotation_window` HAYIR — ikincisi deploy/README'deki bir log alani
#      (`kek_rotation_window=open`) ve her sonek kabul edilince yanlis pozitif verdi
#      (olculdu). `_ROTATED`, `_BACKUP` gibi baska bir sonek kacar.
#  33. AYRACSIZ VE CAMEL SINIRSIZ ADLAR (kapanis, sayildi): anahtar kelimesi bir adin
#      icinde ne `_`/`-` ne de kucuk->Buyuk gecisiyle ayrilmissa baglam sayilmaz —
#      tamami buyuk harf `TAGKEY = "<64 hex>"` ve tamami kucuk harf
#      `tagkey := "<64 hex>"` sessizdir (olculdu): R7D_KEYCTX'in sol siniri `[^a-z]`,
#      R7D_KEYCAMEL ise kucuk->Buyuk gecis ister.
#
# --- BILINEN GURULTU (yanlis POZITIF, tasarim geregi) ---------------------------
# Deger gucu bir anlam testi degildir: `password: from-secret` (YAML duz skaler, 2
# sinif), `openssl ... -pass=file:` + yol (`file:` onekli basvuru), `pass=` + bir
# TARIH (YYYY-AA-GG; 2 sinif) FAIL verir (uc sekil de olculdu). Gorunurdur (CI
# kirmizi, bir insan bakar) ve o SATIRA bagli bir muafiyetle cozulur
# (`bash scripts/secretscan.sh --hash <dosya>:<satir>`; tablonun basligi).
# 10. tur: bir `ANAHTAR=` ardinda Go'nun indeksli fiili (`%[1]s`) ve kacisli yuzde
# (`100%%s`) printf kod seklinin DISINDA kalir; sir kelimeli anahtarla (SESSION_ +
# token) tirnak icinde a0-token verir (olculdu). Ayni yol — satira bagli bir muafiyet.
# 12. tur, ANAHTAR baglami (olculdu; agacta ve gecmiste ornegi 0, hepsi satira bagli
# muafiyetle cozulur): key/HMAC/aes kelimesinin yaninda backtick icinde 64 haneli
# sha256 ozeti; Go `hmacHex := "<64 hex>"`; key yaninda tiresiz bir UUID'nin hex'i;
# key yaninda rakam iceren 32 karakterden uzun bir Go fonksiyon adi ve 22 karakterlik
# bir tanimlayici (buyuk+kucuk harf+rakam); tek tirnakli bir printf bicim dizgesinde
# `PGPASS` + `WORD=%-20s` ardindan `\n` (pw-assign: tek tirnak paritesi kaniti yok).

# R7D_SEP — kayit alan ayiraci (US, 0x1F). Bkz. baslik "KAYIT BICIMI".
R7D_SEP=$'\037'

# R7D_TRIGGERS — a0-token'in "sir kelimesi". Kucuk harfe cevrilmis METIN uzerinde
# awk ERE olarak (LC_ALL=C: bayt bayt, BSD awk ile mawk AYNI davranir), ve (asagida)
# rg on-suzgecinde `-i` ile AYNEN kullanilir; iki sozdizimine de uyan bir alt kume
# secildi (\b yok, {n} yok).
# `s(ı|i)r(...)`: Turkce "sir" (sırrı, sırlar) — ama "sıra" (siralama), "şirket"
# (sirket), "sırf" DEGIL: harf ile devam eden her yazim disarida (olculdu, bu
# kelimeler agacta yuzlerce kez geciyor).
# 🔴 TURKCE BUYUK HARFLER ACIKCA YAZILI (2. tur, N4): mawk'in tolower'i yalniz ASCII'yi
# kucultur, yani `ŞİFRE` -> `Şİfre` kalir ve eski `(ş|Ş|s)ifre` onu KACIRIYORDU
# (olculdu, Ubuntu mawk 1.3.4; BSD awk UTF-8 yerelinde yakaliyordu). Artik awk
# LC_ALL=C altinda kosar ve buyuk/kucuk her Turkce harf alternatif olarak yazilir:
# ş/Ş · i/ı/İ/"i + birlesik nokta" (BSD towlower'in İ ciktisi).
R7D_TRIGGERS='parola|(ş|Ş|s)(i|ı|İ|i̇)fre|password|passwd|passphrase|secret|token|credential|(^|[^a-z])s(ı|i)r(r|la|[^a-z]|$)'

# R7D_KEYCTX — 11. tur (§4.7): ANAHTAR baglami. KEK ve NTAG AES anahtarlari "parola"
# ya da "secret" kelimesiyle anilmaz: "canli KEK", "plaket anahtari (key 1)". Bu
# kelimeler YALNIZ anahtar bicimli tirnakli bir degerle birlikte tetik olur (keyshaped:
# >=32 hex, >=32 harf+rakam, 32 ya da 16 bayta cozulen base64) — `key` cok yaygin,
# kisa ya da kod bicimli degerde gurultu uretmemeli (olculdu). Turkce ekler (`anahtari`,
# `anahtarini`) onek olarak kapsanir.
R7D_KEYCTX='(^|[^a-z])(kek|hmac|aes|key|anahtar)'
# R7D_KEYCAMEL — 12. tur: ayni kelimeler camelCase bir adin ICINDE (`tagKey`, `prodKEK`,
# `sessionHMACKey`, `plaqueKey`). KUCUK harfe cevrilmeden, ORIJINAL metinde aranir:
# kucuk harf ya da rakamdan sonra buyuk harfle baslayan kelime. Yine yalniz anahtar
# bicimli tirnakli bir degerle birlikte tetiktir.
R7D_KEYCAMEL='[a-z0-9](Key|KEY|KEK|Kek|HMAC|Hmac|Aes|AES)'

# R7D_PREFILTER — rg'nin ucuz on-suzgeci: her sinifin OLMAZSA OLMAZ parcasi. Bir
# satir buradan gecmezse hicbir sinif onu eslestiremez; gecmesi hicbir sey
# kanitlamaz, karari awk verir. Yeni bir sinif eklenirse cipasi BURAYA da girer.
R7D_PREFILTER="$R7D_TRIGGERS|kek|hmac|aes|key|anahtar|://|AKIA|ASIA|PRIVATE KEY|[\$]2|pass|pwd|api.?key|access.?key|auth.?token|identified|curl|gh[pousr]_|github_pat_|k_live_|xox[bpa]-|glpat-|AIza|eyJ"

# R7D_WAIVERS — MUAFIYET TABLOSU (7. tur): muafiyet SATIRIN TAMAMINA baglidir.
# Satir basina:  <yol ERE>;<sha256>;<kisa aciklama>
#
# Bir satir ancak (1) yolu bir girdinin `^...$` ile capali ERE'sine uyuyorsa VE (2)
# satirin TAM iceriginin sha256'si o girdininkiyle BIREBIR ayniysa muaftir; o zaman
# HIC siniflanmaz ve WARN'da `yol:satir: [muaf]` gorunur. Hash satirin butun
# baytlari uzerindendir: yalniz satir sonu `\n` haric — `\r` ve sondaki bosluk
# DAHIL, normalizasyon YOK. Satira NERESINDEN olursa olsun bir bayt eklenir,
# cikarilir ya da degisirse hash tutmaz ve satir ADSIZ bir yoldaki gibi siniflanir
# (TestSecretScan_AWaiverIsBoundToTheWholeLine). Ayni dosyada bayt bayt AYNI bir
# satir (bir kopya) ayni girdiyle muaftir: yeni bir deger tasiyamaz.
#
# NEDEN (7. tur, orkestrator karari). Alti tur boyunca her bloklayan bulgu AYNI
# siniftandi: satirin ICINDEKI bir belirteci affedip geri kalanini siniflamaya
# calismak — onek (1), ayni satir ayraclari (2), satirlar arasi (3), tirnak paritesi
# (4), belirtecin icindeki sir kelimesi (5), bosluga cevirmenin actigi SOL uzatma
# (6; ucuncu goz 22 belirtec x 5 ayrac x 3 tirnak = 330 satirin 330'unu adli yolda
# sessiz olctu). Satir-ici mekanizma — sinir kurallari, bosluga cevirme, baglam
# aktarimi, `@prefix`/`@class` turleri, tablonun kendi satiri istisnasi — SILINDI.
# redline-check.sh R1'in "cumleye bagli" ilkesinin satira uygulanmasi.
#
# MUAFIYET NASIL EKLENIR. Satir bir FAIL veriyor ve gercekten sir degilse:
#   bash scripts/secretscan.sh --hash <depo-goreli dosya>:<satir no>
# tabloya yapistirilacak `^<yol>$;<sha256>;` basini basar — satirin METNINI basmaz.
# Sonuna DEGER ve sir kelimesi (R7D_TRIGGERS) ICERMEYEN kisa bir aciklama yazilir.
# Aciklamada sir kelimesi ya da DEGER bicimli bir parca (8. tur: aciklama bir satir
# gibi siniflanir ve her parcasi a0 adayi gibi sinanir), 64 kucuk hex olmayan hash,
# TEK bir dosyayi adlandirmayan yol (8. tur: yalniz `^`, harf/rakam/`_`/`-`/`/`/`@`,
# `[.]`, `$`), bos aciklama ya da ikinci kez yazilmis ayni (yol, hash) tabloyu exit 2
# ile reddettirir. Satir sonradan
# degisirse muafiyet duser ve satir yeniden FAIL verir — bilincli: yeni metni birinin
# yeniden bakip muaf tutmasi gerekir. Commit/tag mesaji icin muafiyet YOK (olculmus
# bir ihtiyac yok); yol ERE'si `commit-msg@<sha>`'yi de adlandirabilir.
#
# Tablonun KENDI satirlari (bu dosyada) deger tasimaz: yol, hash, aciklama. 64 haneli
# hex tirnaksizdir ve satirda sir kelimesi olsa bile a0-token'in hex alt-sekli
# yalniz TIRNAKLI adaya bakar — olculdu, ozel bir istisna gerekmedi (6. turdaki
# "tablonun kendi satiri" mekanizmasi SILINDI).
#
# ⚠️ `read -d ''`, `$(cat <<EOF)` DEGIL: bash 3.2 (macOS /bin/bash) komut ikamesi icindeki
# heredoc'u ayristirirken govdedeki tek tirnaklari SAYAR ve dosyanin geri kalanini
# tirnak icinde okur — olculdu, `syntax error` ile duser.
# read EOF'ta 1 doner; `|| true` bu yuzden (tablo yine de tam okunmustur).
IFS= read -r -d '' R7D_WAIVERS <<'R7DW' || true
# Agactaki 37 muaf satirdan uretildi (6. turun belirtec tablosunun muaf ettigi satirlar,
# birebir); adminreset_test'te bayt bayt ayni satirlar tek girdi: 32 girdi.
^[.]env[.]example$;81dcb21674027b03de11215bbae96a9f58e01e59c24f21b87dea404c03f9f5ec;yerel dev DSN, uygulama rolu (docker-compose Postgres)
^[.]env[.]example$;06eda7f6c96ee04629e2442dbfaa8d2b822ec9699c53ec6e6513d144bce4986d;yerel dev DSN, sahip rolu (migration)
^[.]github/workflows/ci[.]yml$;c6ab16dfc096ff84d51c78941a7a233bf4371ad2b7c612f9c5994cc11532403a;CI gecici konteyner DSN, uygulama rolu
^[.]github/workflows/ci[.]yml$;fc3c428517e1fc3675807b2f225902113cb103d0961906613dd14d6f8ef13b6e;CI gecici konteyner DSN, sahip rolu
^[.]github/workflows/ci[.]yml$;a4df4aee4c935f3364e01efb19631f5a9152f52fd09488d4ada288af1a1a35f1;CI-only sahte KEK aciklama yorumu
^android/app/src/test/kotlin/mt/taptime/relay/RelayLoopTest[.]kt$;34c9ac68a9db4d3680d6eee118f4656d6227346a4a2d86a891187c5f43974028;sentetik kimlikli URL: hataya sizmama testi
^cmd/tappa/serving_test[.]go$;e21d1e27658545b207bd4419c7105f6426c033f99dd9bf9804ac4f7e33c8d349;sentetik DSN: hataya sizmama testi
^docs/adr/0019-parola-min-8[.]md$;803f793b48d83c8505901d66899b87ca9e7dd377068722418191982be849a36e;ADR 0019 kanonik zayif ornek
^docs/plan/m10-platform[.]md$;c92e0a6843f4f45528605072a43ca748c34ff888e5d9158a41649439e848fdc4;F0-5 kartinin mutasyon ornegi (tek harfli DSN)
^internal/adminauth/password[.]go$;8106435b466bc478fee9ac4f27e53b67f4e3139ceef5cae41727f0082e11439a;dummyDigest: atilmis rastgele dizenin bcrypt ozeti
^internal/adminauth/password_test[.]go$;606b3933e41fec5e97a022039f62e9aec58c1363946afd467555606e110ca4c6;seed demo sahibi bcrypt ozeti (dev)
^internal/adminauth/password_test[.]go$;eaa58695bb3013e55a543663dd69703dec07167f530e94d99e8b189b52620e78;seed demo sahibi bcrypt ozeti (dev)
^internal/db/invites_test[.]go$;f9528999755c8b153fc1346f1edb7dc0bc1237917d55deee1b2c7aca51d34b0a;sentetik base64 test degeri (davet kodu testi)
^internal/domain/signup/signup[.]go$;9957e14760cfb4071997f4bc754974972ccc1075839697c1399a984f757dbb3d;ADR 0019 kanonik zayif ornek (yorum)
^internal/encode/rows_db_test[.]go$;39c2f60a96559bba116842cbf37812f2ef559f4818c7a461ea5e53b34a9498e0;alfabe dolgusu, bcrypt bicimli
^internal/handler/adminlogin_db_test[.]go$;906e4ceca519293d8b1697c23be5145a095c4c6fb2db33ef123057a64f9a436c;sentetik test bcrypt ozeti
^internal/handler/adminlogin_db_test[.]go$;ee1991f8375fecadff1e7ccae57b6d210a732c8462beb9bcf869f528b8eed94f;sentetik test bcrypt ozeti
^internal/handler/adminreset_test[.]go$;6ece128e489085ef0306f3be8ca7ae84770ddb3cfdd93bf7e119296935abb80a;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;844dca6e2715e0b8326f83f26b0a08a0886617e686d69b1ab77284937648f22f;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;1f2619e8660e1670335bccceb2877e3804a88aa824bc958d81313166ddbc248d;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;3c4aeb4e211a8f7cfe8010bd06cc002a0dfd04a7c996adf7564449c87b85ba55;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;6d1b99ba8f5874177e577f878351b725c563dca84e195f05954d37127e45e818;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;e3d940bed0252135722a53aec6ce0052dce36891f04bf1ab6badd3fc8f17a3a3;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;3132f2c4a3c624e073822b9a7c0d94579aa7f93a457dbc288843789f09f78311;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;0135c768d87f458e87c5320447d29a4c3fd694a7de5f7485e26f6143d4839b07;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;59478a0ee7db4c9ee6c286307c5bf1e059c51d788dacbf07a42c1d0a87605fab;sentetik sifirlama akisi test degeri
^internal/handler/adminreset_test[.]go$;8f451658979cdd418b80dda3bf93a695edfbf8479a0872aa609c07a46620aefd;sentetik sifirlama akisi test degeri
^internal/handler/health_db_test[.]go$;dcd571e17305d780051fcaf1f541eaf592a07fdf663c8c41d237fc358720e71d;sentetik DSN: hataya sizmama testi
^internal/handler/logincontext_test[.]go$;d3b8bd29b16d78c5663e9e6ef22ffd74414de62b8c1c1baf55a42f71584fc483;sabit zamanli karsilastirma testi
^test/fixtures/seed[.]sql$;fc3bde1e9e86d1626bb484ab9683cd47f9dbde09746750759deeb303e4c6b242;seed demo sahibi bcrypt ozeti (dev)
^test/fixtures/seed[.]sql$;3f6973aef02bc0e14af2eb9ecaf4e2278ca15a79b80c4fbfcc39360737fb3397;seed demo sahibi bcrypt ozeti (dev)
^web/static/vendor/htmx[.]min[.]js$;71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de;vendored htmx, tek satir: hash = README sha256
# 11. tur (§4.7): ANAHTAR baglami (R7D_KEYCTX) ve kek/hmac_key atamalari bunlari da
# yakaladi — hepsi siniflandirildi, hicbiri gercek bir anahtar degil: CI'nin belgelenmis
# sahte KEK'i (ci.yml'deki aciklamanin base64'u; cozulup karsilastirildi), NXP AN12196
# uygulama notunun herkese acik bilinen-cevap vektorleri, `_label: FAKE` fixture.
^[.]github/workflows/ci[.]yml$;ccb2538b430058e903abc20579e12cb1e56548100b257989a4ab60a893f09aba;CI-only sahte KEK degeri: belgelenmis sabit metnin base64'u (CI gecici konteyneri)
^internal/sun/an12196_kat_test[.]go$;3aa15447924c69a28794bcb6196f3cd450801f78f0cb85e4d5187155ca8b2227;AN12196 bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;eaafae979a5b757669ba35fb40ca12cb3092d1a49793617047249ab065b4c647;AN12196 ChangeKey bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;d1f64f7bd688d9089cc730cec050244e7c17626dde00dace39c14e78aec4e861;AN12196 ChangeKey bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;492e999df2cf40a70802b7048786d55a4a69aea8ebfa5cb7a29511b79c380e2a;AN12196 ChangeKey bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;69ef158de106eb61ec136ceb1fcad0af16f3a18a55d80e28fe62b14c36aec386;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;9ae98cf421e2a3e70d89a4e8320ff5daf51d869fb1dd5c5c4b68131a4dee2dde;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;0d612561a291ef5ce18f124b5e4a54d5a248e31289b796993936e93844413d77;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;ba7ebf722c71fb643b385a13a4a6c3f98476c00896d096374cdd726eb59cc770;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;24a16b011eac221757d8efaced1e45070edb2fe14007b768a0b78a71ec7ecb2d;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;63862875524d0dc3b5b601e63f0be1a0b6416697602cb2258ca9470b3d01ce13;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;9af24b3da3ac22631286f262fabee5208c7caad77a4701ed0d71d374bc9bbd91;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/ev2_kat_test[.]go$;0feb71be0c41012a2782efcdf84c006c3613585000a49c9d4b0c1226c68d7703;AN12196 EV2 oturum bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/verify_mac_test[.]go$;de1b4a528b32bf8d637c7ac45f47f5b8026e075a80c49ae36ba92e4148410e0b;sahte test plaket degeri (verify testi, farkli sahte anahtar)
^test/fixtures/sun_vectors[.]json$;d466088fc3946135e5f4ac11c4c136da7f861f62f7f8370a5cc0cb53f00e5091;sahte test fixture degeri (sun_vectors.json _label: FAKE)
^test/fixtures/sun_vectors[.]json$;2f3f29c96f7856ee99f11521c8a2475431227277b79bb380a598a84457c63700;sahte test fixture degeri (sun_vectors.json _label: FAKE)
^test/fixtures/sun_vectors[.]json$;4cfe25a7b5d56aabc9299a7f60ceb9553cf1951925ce74d27d35c358e991949c;sahte test fixture degeri (sun_vectors.json _label: FAKE)
^test/fixtures/sun_vectors[.]json$;58b9da54498b464234eb08ee7b63c2453a2db8370ef371ac5d45969b9f8a54aa;sahte test fixture degeri (sun_vectors.json _label: FAKE)
# 12. tur (§4.7): camelCase anahtar baglami (R7D_KEYCAMEL: tagKey, fakeKEKHex, ...)
# bunlari da yakaladi — kaynaginda siniflandirildi, hicbiri gercek bir anahtar degil:
# AN12196 vektorleri, belge disi bir bayt rampasi, adinda fake/test/wrong gecen test
# degerleri, sun_vectors.json'daki sahte degerlerin kopyalari (deger esitligi olculdu).
^internal/handler/adminlogin_db_test[.]go$;9a36528d5f9fc5e486f3ebc7c8beb25fc9b031615b0a5a1261322e67d9d80238;sentetik test HMAC degeri (davet fixture, oturumdan ayri)
^internal/handler/logincontext_test[.]go$;fd3cb0c2acb6fd0190349a0b98d4d9d59368cfc597afe8d31854e1fa2517bd43;sentetik test HMAC degeri (farkli oturum testi)
^internal/handler/tap_db_test[.]go$;015e1afff896440b13b9e615bb546e87805131c98d06cc3851660fd276d10763;sun_vectors.json sahte KEK ile ayni sahte deger
^internal/handler/tap_db_test[.]go$;1a22c9ac826c899ccc50f5fb9a55a32bd9ac5278b1d7fa98c1c091b9869fb58d;sun_vectors.json sahte tag_key_A ile ayni sahte deger
^internal/invite/code_test[.]go$;12067ab55839ce14b986e104d96ec136d6b105b3a053cfb11504b27cee290563;sentetik test degeri (davet kodu testi, tek bayt farkli ikili)
^internal/sun/an12196_kat_test[.]go$;bdde8ea736dd1dace85c70e9bbc182d91fc4941677d18004908bbd154212ae6f;AN12196 bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;db8d54e71d2225993dd88009172cf8ea1d858db839394913f10029e98fb3776d;AN12196 ChangeKey bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;839bf4e3ba04b77251ce46219b556e22f04584e6f925b69f6918b675776a0a41;AN12196 ChangeKey bilinen-cevap vektoru (NXP uygulama notu, herkese acik)
^internal/sun/changekey_test[.]go$;044431b3ee7cddf7f64d7e36dd7681d96cd28fa8c0f64d64e29d11af6f17cf37;bayt rampasi, belge disi sentetik eski deger (ChangeKey kurtarma yolu testi)
^internal/sun/keys_test[.]go$;85b6505fec3ecc3b3c76bf5b1da056167e3238c3a2117e55831932c41492c592;sun_vectors.json sahte KEK ile ayni sahte deger
^internal/sun/keys_test[.]go$;bc2dfa6189ab01962a0f3ffecdb4b83eca30d68d7628867a890ba9712a6825cd;sahte test KEK degeri (ikinci, adinda fake)
^internal/sun/preview_test[.]go$;b487e373b59ed2f4cd6b830f4b953ae6d1f8cb0c9ad20d5b26908749007982a6;sahte test KEK degeri (yanlis KEK testi)
^internal/sun/rotate_test[.]go$;9c99bc95e3395488dd120089259b6c4de81ec4b90c468d50016300fec60b2314;sahte test KEK degeri (ucuncu, adinda fake)
^internal/sun/verify_mac_test[.]go$;0c355ab3b688a40cd24187493a5c1a05800a39ad79d2b72cc1594fd0e6e301c1;sun_vectors.json sahte tag_key_A ile ayni sahte deger
R7DW

# r7d_hasher — sha256 araci: sha256sum ya da `shasum -a 256`. Bilinen bir vektorle
# sinanir (surec basina bir kez); calismayan bir arac "tablo hic eslesmedi" demek
# olurdu, yani tarama exit 2.
R7D_HASHER=""
r7d_hasher() {
  local h
  [[ -n $R7D_HASHER ]] && return 0
  for h in "sha256sum" "shasum -a 256"; do
    # shellcheck disable=SC2086 # h bilerek bolunur (arac + argumanlari)
    if command -v "${h%% *}" >/dev/null 2>&1 &&
      [[ $(printf abc | $h 2>/dev/null) == ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad* ]]; then
      R7D_HASHER=$h
      return 0
    fi
  done
  echo "secretscan: sha256 araci yok ya da calismiyor (sha256sum / shasum -a 256) — TARAMA GUVENILIR DEGIL" >&2
  return 2
}

# r7d_select <fail|waived> — stdin: `yol US satir US metin` kayitlari (R7D_SEP).
# stdout: `yol:satir: [sinif,...]` (fail) ya da `yol:satir: [muaf]` (waived) — METIN
# YOK (basliktaki 🔴). Once yolu bir tablo girdisine uyan kayitlar sayilir; hic yoksa
# dogrudan siniflandirilir. Varsa her birinin metni gecici bir dizine TEK dosya olarak
# yazilir, hepsi TEK bir sha256 cagrisiyla hash'lenir ve siniflandirma hash'i
# tablodakiyle ayni olan kaydi muaf sayar.
# Donus: 0 · 2 = tablo bozuk, ayrisamayan kayit, sha256 araci yok ya da hash sayisi
# tutmadi. Cagiran bunu OKUMAK ZORUNDA: $(...) icinde kaybolan bir hata "temiz" okunur
# (R1'in SCAN_ERR dersi; 2. tur B2 ayni dersi kancanin boru hattinda yeniden ogretti).
r7d_select() (
  # 11. tur (§4.7): govde bir ALT KABUKTUR ve kendi tuzaklarini kurar. Gecici dizin
  # muafiyet yolundaki satirlarin TAM metnini tasir; SIGINT/SIGHUP ile kesilen bir
  # tarama onu TMPDIR'da birakiyordu (olculdu: `$(...)` alt kabugu cagiranin EXIT
  # tuzagini miras almaz). Alt kabuk: tuzaklar cagiran kabugunkileri EZMEZ. Olculdu:
  # SIGINT'te bash 3.2 ve 5.2 EXIT tuzagini kendisi kosar; SIGHUP'ta ikisi de, SIGTERM'de
  # bash 3.2 kosmaz — o ikisi `exit`e cevrilir.
  mode=$1; in=""; n=""; tmp=""; rc=0
  trap '[[ -n $tmp && -d $tmp ]] && rm -rf "$tmp"' EXIT
  trap 'exit 129' HUP
  trap 'exit 143' TERM
  IFS= read -r -d '' in || true
  n=$(r7d_awk count "" <<<"$in") || exit 2
  if [[ $n == 0 ]]; then
    r7d_awk "$mode" "" 0 <<<"$in"
    exit
  fi
  r7d_hasher || exit 2
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/tappa-r7d.XXXXXX") || {
    echo "secretscan: gecici dizin yaratilamadi — TARAMA GUVENILIR DEGIL" >&2
    exit 2
  }
  # shellcheck disable=SC2086 # R7D_HASHER bilerek bolunur
  if r7d_awk pick "$tmp" <<<"$in" && $R7D_HASHER "$tmp"/c.* >"$tmp/h"; then
    r7d_awk "$mode" "$tmp" "$n" <<<"$in" || rc=$?
  else
    echo "secretscan: muafiyet hash'leri hesaplanamadi — TARAMA GUVENILIR DEGIL" >&2
    rc=2
  fi
  exit "$rc"
)

# r7d_awk <count|pick|fail|waived> <gecici dizin> [hash sayisi] — tek awk programi:
# count = tabloda yolu olan kayitlari say, pick = onlarin metnini dosyalara yaz,
# fail/waived = siniflandir.
# awk programi TEK TIRNAK icinde: icinde kesme isareti YOK (R1'de iki kez
# sessizce bos donen hata). Tek tirnak karakteri programa SQ degiskeniyle girer;
# tablo ve tetikleyiciler ENVIRON ile girer, cunku -v degeri ters egik cizgi
# kacislarini isler ve BSD awk -v icinde satir sonunu kabul etmez.
# Tasinabilirlik: BSD awk (macOS) + mawk (Ubuntu CI) — {n} tekrari, \b ve POSIX
# [[:sinif:]] KULLANILMAZ. LC_ALL=C: iki awk da bayt bayt calisir (N4).
r7d_awk() {
  LC_ALL=C R7D_ENV_TRIG="$R7D_TRIGGERS" R7D_ENV_KEYCTX="$R7D_KEYCTX" R7D_ENV_KEYCAMEL="$R7D_KEYCAMEL" R7D_ENV_WAIVERS="$R7D_WAIVERS" awk -v mode="$1" \
    -v dir="$2" -v hf="$2/h" -v nc="${3:-0}" -v SQ="'" '
    BEGIN {
      SEP = "\037"
      TRIG = ENVIRON["R7D_ENV_TRIG"]
      KEYCTX = ENVIRON["R7D_ENV_KEYCTX"]
      KEYCAMEL = ENVIRON["R7D_ENV_KEYCAMEL"]
      Q[1] = "\""; Q[2] = SQ; Q[3] = "`"
      nw = 0; bad = 0
      n = split(ENVIRON["R7D_ENV_WAIVERS"], L, "\n")
      for (i = 1; i <= n; i++) {
        if (L[i] ~ /^[ \t]*(#|$)/) continue
        # 9. tur (N5): hata iletisi satirin METNINI basmaz — alanlardan birine yanlislikla
        # bir deger yazilmis olabilir. Yalniz tablo satir NUMARASI ve bozuk ALANIN adi.
        a = index(L[i], ";"); rest = substr(L[i], a + 1); b = index(rest, ";")
        wp = substr(L[i], 1, a - 1); wh = substr(rest, 1, b - 1); wd = substr(rest, b + 1)
        if (!a || !b) tablo_hata(i, "alan ayraci: <yol ERE>;<sha256>;<aciklama> bekleniyordu")
        if (length(wh) != 64 || wh !~ /^[0-9a-f]+$/) tablo_hata(i, "sha256: 64 kucuk hex bekleniyordu")
        if (wd !~ /[^ \t]/) tablo_hata(i, "aciklama bos")
        # 8. tur: yol TEK bir dosyayi adlandirir — `^` + harf/rakam/`_`/`-`/`/`/`@` ya da
        # `[.]` + `$`. Kacissiz `.`, `*`, `+`, `(`, `|` gibi bir regex meta karakteri
        # (`^.*$` butun agaci adlandirirdi) tabloyu reddeder.
        if (wp !~ /^\^([A-Za-z0-9_\/@-]|\[[.]\])+[$]$/) tablo_hata(i, "yol tek bir dosya adlandirmiyor (kacissiz regex meta karakteri)")
        if (tolower(wd) ~ TRIG) tablo_hata(i, "aciklama bir sir kelimesi tasiyor (R7D_TRIGGERS)")
        # 8. tur: aciklama bir DEGER tasiyamaz — sir kelimesi olmasa da. Aciklama bir satir
        # gibi siniflandirilir (url-cred, bcrypt, ...) ve her parcasi a0-token adayi gibi
        # sinanir: hem deger karakteri olmayan her seyde bolunmus parcalar, hem (9. tur,
        # N3) YALNIZ bosluklarla bolunmus kelimeler — `Kx9!pQ2` + `(`/`,`/`;`/`[`/`{`/`<`/
        # `\`/ASCII disi harf + `wLm3Zq` bolucusuyle asiliyordu (olculdu).
        SIG = "plain"
        if (classes(wd, "aciklama") != "" || strongpiece(wd) || strongword(wd)) tablo_hata(i, "aciklama deger bicimli bir parca tasiyor")
        if ((wp, wh) in seen) tablo_hata(i, "ayni (yol, sha256) ikinci kez")
        seen[wp, wh] = 1
        nw++; wpath[nw] = wp; whash[nw] = wh
      }
      npath = 0
      if ((mode == "fail" || mode == "waived") && nc + 0 > 0) {
        nh = 0
        while ((rc = (getline ln < hf)) > 0) {
          h = substr(ln, 1, 64); nm = ln; sub(/^.*\/c[.]/, "", nm)
          if (length(h) != 64 || h !~ /^[0-9a-f]+$/ || nm !~ /^[0-9]+$/) { nh = -1; break }
          H[nm] = h; nh++
        }
        if (rc < 0 || nh != nc + 0) {
          print "secretscan: sha256 ciktisi beklenen sayida degil (" nh "/" nc ") — TARAMA GUVENILIR DEGIL" > "/dev/stderr"; exit 2
        }
        close(hf)
      }
    }
    function add(c, x) { return (c == "") ? x : c "," x }
    function tablo_hata(i, neden) {
      print "secretscan: muafiyet tablosunun " i ". satiri bozuk: " neden > "/dev/stderr"
      exit 2
    }
    # strongword: YALNIZ bosluklarla bolunmus her kelime; ASCII disi baytlar ve
    # ayrac/tirnak/ters egik cizgi atilip kalan birlesik metin a0 adayi gibi sinanir.
    function strongword(s,    n, k, P, w) {
      n = split(s, P, /[ \t]+/)
      for (k = 1; k <= n; k++) {
        w = P[k]; gsub(/[^!-~]/, "", w); gsub(SQ, "", w); gsub(/[\\"`()<>{},;]|\[|\]/, "", w)
        if (secretish(w)) return 1
      }
      return 0
    }
    # strongpiece: metni deger karakteri olmayan her seyde (bosluk, tirnak, backtick,
    # parantez, virgul, noktali virgul) boler; secretish bir parca varsa 1.
    function strongpiece(s,    n, k, P) {
      gsub(SQ, " ", s)
      n = split(s, P, /[^A-Za-z0-9!#$%&*+.\/:=?@^_|~-]+/)
      for (k = 1; k <= n; k++) if (secretish(P[k])) return 1
      return 0
    }
    function onpath(p,    k) { for (k = 1; k <= nw; k++) if (p ~ wpath[k]) return 1; return 0 }
    function waived(p, h,    k) { for (k = 1; k <= nw; k++) if (p ~ wpath[k] && h == whash[k]) return 1; return 0 }
    # sigmode: bu yolun metninde `$` nasil acilir (6./7. tur B3). "shell": kabuk
    # betikleri, Compose/CI YAML, Makefile, Dockerfile, .env. "k8s": deploy/k8s YAML
    # manifestleri — orada YALNIZ `$(AD)` acilir, `$X` ve `${X}` harfi harfine degerdir
    # (7. tur, olculdu). "plain": geri kalan her sey (.md, commit/tag mesaji, kaynak kod).
    function sigmode(p) {
      if (p ~ /^deploy\/k8s\/.*[.]ya?ml$/) return "k8s"
      if (p ~ /[.](sh|bash|zsh|ksh|ya?ml|mk|env)$/) return "shell"
      if (p ~ /(^|\/)(Makefile|GNUmakefile|Dockerfile[^\/]*|[.]env[^\/]*|[.]envrc)$/) return "shell"
      if (p ~ /^scripts\/git-hooks\//) return "shell"
      return "plain"
    }
    function distinct(s,    k, c, n, got) {
      n = 0
      for (k = 1; k <= length(s); k++) { c = substr(s, k, 1); if (!(c in got)) { got[c] = 1; n++ } }
      return n
    }
    # sigilref: `$`/`%` ile baslayan deger bir BASVURU mu — `${X}`, `$(..)`, `$1`, `$X`,
    # `%s`, `%(ad)s`, `%-20s`. 5. tur N2: eskiden `$`/`%` ile baslayan HER deger yer
    # tutucuydu; `$` + guclu govde (`$Zq9w!...`) alti baglamda sessizdi (olculdu).
    # `$AD` acilir; ARDINDAKI harfi harfine kalan (`$HOME/.kube/config` -> yol) guclu
    # ve kod bicimi disi ise deger DEGERDIR. (Tam gecmis taramasi olctu: kalan kisma
    # bakmayan ilk hali deploy.yml gecmisindeki `"$HOME/..."` yolunu yakaliyordu.)
    # 6. tur B3: bu kural YALNIZ `$` i gercekten ACAN baglamda (kabuk, Compose/YAML,
    # Makefile, .env; sigmode) gecerli. DUZ baglamda (.md, commit/tag mesaji, kaynak
    # kod dizgesi, URL parolasi) `$` hicbir sey acmaz: yalniz `$AD`, `${...}` ve printf
    # bicimi (`%s`, `%-20s`, `%(ad)s`) basvurudur; baska her sey `$`/`%` DAHIL TAM
    # deger olarak olculur (olculdu: A-0 biciminde `$` ile baslayan kisa parola kisa
    # kuyruk kurali yuzunden sessizdi, `$` siz hali FAIL).
    function sigilref(v, m,    b) {
      b = substr(v, 2)
      if (m == "k8s") return (v ~ /^[$][(][A-Za-z_][A-Za-z0-9_]*[)]$/)
      if (m == "plain") {
        if (b ~ /^[A-Za-z_][A-Za-z0-9_]*$/ || v ~ /^[$][{].*[}]$/) return 1
        return (v ~ /^%[-+ #0]*[0-9*]*([.][0-9*]+)?[a-zA-Z]$/ || v ~ /^%[(][A-Za-z_][A-Za-z0-9_]*[)][-+ #0]*[0-9]*([.][0-9]+)?[a-zA-Z]$/)
      }
      if (b ~ /^[{(0-9]/) return 1
      if (match(b, /^[A-Za-z_][A-Za-z0-9_]*/)) b = substr(b, RLENGTH + 1)
      if (b == "") return 1
      return (length(b) < 8 || classes2(b) < 3 || codeish(b))
    }
    # placeholder: lit=1 ise deger `$`/`%` ACMAYAN bir baglamdan geliyor (kabugun tek
    # tirnagi, .sql dosyasindaki SQL dizgesi): orada `$X` bir degiskene basvuru degil,
    # harfi harfine degerdir.
    # m: sigilref ile ayni (sigmode).
    function placeholder(v, lit, m,    lv) {
      lv = tolower(v)
      if (v == "") return 1
      if (v ~ /^[$%]/) { if (!lit && sigilref(v, m)) return 1 }
      else if (v ~ /^[<{]/) return 1
      if (index(v, "…") || index(v, "...")) return 1
      if (lv ~ /^[*x]+$/ || lv ~ /^(changeme|redacted|placeholder)$/) return 1
      if (v ~ /^REPLACE_[A-Z0-9_]*$/ || v ~ /^[A-Z0-9_]*PASSWORD[A-Z0-9_]*$/) return 1
      return 0
    }
    function c_url(t,    s, i, a, k, at, ui) {
      s = t
      while ((i = index(s, "://")) > 0) {
        s = substr(s, i + 3); a = s
        if (match(a, /[\/ \t"`<>\\]/)) a = substr(a, 1, RSTART - 1)
        k = index(a, SQ); if (k) a = substr(a, 1, k - 1)
        at = 0
        for (k = length(a); k > 0; k--) if (substr(a, k, 1) == "@") { at = k; break }
        if (!at) continue
        ui = substr(a, 1, at - 1)
        k = index(ui, ":")
        if (k && !placeholder(substr(ui, k + 1), 0, "plain")) return 1
      }
      return 0
    }
    function c_aws(t,    s, pre) {
      s = t
      while (match(s, /(AKIA|ASIA)[0-9A-Z]+/)) {
        pre = (RSTART > 1) ? substr(s, RSTART - 1, 1) : " "
        if (RLENGTH == 20 && pre !~ /[A-Za-z0-9]/) return 1
        s = substr(s, RSTART + RLENGTH)
      }
      return 0
    }
    function c_pem(t) { return (t ~ /-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY( BLOCK)?-----/) }
    function c_bcrypt(t,    s, b) {
      s = t
      while (match(s, /[$]2[abxy]?[$][0-9][0-9][$][.\/A-Za-z0-9]+/)) {
        b = substr(s, RSTART, RLENGTH); sub(/^[$]2[abxy]?[$][0-9][0-9][$]/, "", b)
        if (length(b) == 53) return 1
        s = substr(s, RSTART + RLENGTH)
      }
      return 0
    }
    # c_known: saglayici onekli belirtecler. Solunda harf/rakam/_ olan eslesme bir
    # kelimenin ortasidir (xghp_...), sayilmaz.
    function c_known(t,    s, tok, pre, body, need, P) {
      s = t
      while (match(s, /(gh[pousr]_|github_pat_|[sr]k_live_|xox[bpa]-|glpat-|AIza)[A-Za-z0-9_-]+/)) {
        tok = substr(s, RSTART, RLENGTH)
        pre = (RSTART > 1) ? substr(s, RSTART - 1, 1) : " "
        s = substr(s, RSTART + RLENGTH)
        if (pre ~ /[A-Za-z0-9_]/) continue
        if (tok ~ /^gh[pousr]_/)        { body = substr(tok, 5);  need = 36 }
        else if (tok ~ /^github_pat_/) { body = substr(tok, 12); need = 22 }
        else if (tok ~ /^[sr]k_live_/) { body = substr(tok, 9);  need = 20 }
        else if (tok ~ /^xox[bpa]-/)   { body = substr(tok, 6);  need = 10 }
        else if (tok ~ /^glpat-/)      { body = substr(tok, 7);  need = 20 }
        else                           { body = substr(tok, 5);  need = 35 }
        if (length(body) >= need && distinct(body) > 4) return 1
      }
      s = t
      while (match(s, /eyJ[A-Za-z0-9_-]+[.]eyJ[A-Za-z0-9_-]+[.]/)) {
        tok = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
        split(tok, P, ".")
        if (length(P[1]) >= 10 && length(P[2]) >= 10) return 1
      }
      return 0
    }
    # val_at: (5) belge/yapilandirma degerini okur; acilan tirnak atlanir ve VQ=1
    # (tirnakli deger). 4. tur: buradaki "kapanan tirnak" (parite) denetimi SILINDI —
    # `=` atamalari artik shword ile okunuyor ve parite orada; val_at icin agacta da
    # testte de ayirt edici bir vaka yoktu (olculdu: kaldirinca agac sonucu ayni).
    function val_at(t, pos,    c, v, k) {
      c = substr(t, pos, 1); VQ = 0
      if (c == "\"" || c == SQ || c == "`") { pos++; VQ = 1 }
      v = substr(t, pos)
      if (match(v, /[ \t"`<>&;,(){}|\\]/)) v = substr(v, 1, RSTART - 1)
      k = index(v, SQ); if (k) v = substr(v, 1, k - 1)
      return v
    }
    # shword: `=`in ardindaki KABUK KELIMESI — bitisik tirnakli parcalar ve `\x` birlesir
    # (tappa + tek tirnakli Zq9 -> tappaZq9; 3. tur denetiminin kabuk uzatmasi). Hemen ardindaki
    # tirnak satirda TEK sayida gecmisse KAPANAN tirnaktir (bir kod araliginin ya da
    # dizge sabitinin sonu) ve deger BOSTUR. Tirnaksiz kisim bosluk, `;&|<>()` ve
    # backtick ile biter. SWQ=1: kelimenin bir parcasi tirnakliydi (bir dizge sabiti,
    # cagri/secici olamaz). SWSQ=1: ilk karakteri TEK tirnak icindeydi (`$` acilmaz).
    # ind: anahtarin KENDISI bir cift tirnakli dizgenin icinde (onunde tek sayida `"`):
    # o zaman bir `"` kelimeyi BITIRIR, yeni bir parca acmaz (6. tur, olculdu:
    # Go `Sprintf("...=%-20s", v)` ve `echo "K=v" >> f` dizgenin disini degere katiyordu).
    # Yalniz cift tirnak: duz yazidaki kesme isareti tek tirnak paritesini bozar.
    # 7. tur (Bulgu 2): kapanan `"` ancak ardindan bir kelime ya da tirnak karakteri
    # GELMIYORSA biter; `"K=tappa"'Zq9'`, `"K=tappa"Zq9`, `"K=tappa"\Zq9`, `"K=a""b"`
    # kabukta TEK kelimedir (olculdu: bu bicimler sessizdi) — dizge kapanir, kelime surer.
    function shword(t, pos,    c, v, q, pre, nq, ind, d) {
      SWQ = 0; SWSQ = 0
      c = substr(t, pos, 1)
      if (c == "\"" || c == SQ || c == "`") {
        pre = substr(t, 1, pos - 1); nq = gsub(c, c, pre)
        if (nq % 2 == 1) return ""
      }
      pre = substr(t, 1, pos - 1); ind = (gsub(/"/, "\"", pre) % 2 == 1)
      v = ""; q = ""
      while (pos <= length(t)) {
        c = substr(t, pos, 1)
        if (q != "") {
          if (c == q) q = ""
          else { if (v == "" && q == SQ) SWSQ = 1; v = v c }
          pos++; continue
        }
        if (c == "\"" && ind) {
          d = substr(t, pos + 1, 1)
          if (d == "" || d ~ /[ \t,;)}\]|&<>+\r]/) break
          ind = 0; pos++; continue
        }
        if (c == "\"" || c == SQ) { q = c; SWQ = 1; pos++; continue }
        if (c == "\\") { v = v substr(t, pos + 1, 1); pos += 2; continue }
        if (c ~ /[ \t;&|<>()`\r]/) break
        v = v c; pos++
      }
      return v
    }
    # yamlval: YAML satir basi `anahtar: deger` in DEGERI. Once dugum OZELLIKLERI atlanir
    # (5. tur N4): cipa `&ad` ve etiket `!x`/`!!x`, ardindan bosluk. Tek tirnakliysa
    # iki tek tirnak bir tirnaktir, cift tirnakliysa `\` kacistir (5. tur N1: ikisi de
    # degeri kesiyordu, olculdu); YQ=1. Duz skalerse SATIRIN GERI KALANI (bosluk, `,`,
    # `;`, tirnak, `(` ve `\` duz skalerin parcasidir — 3. tur denetiminin sekiz ayraci),
    # ` #` yorumuna kadar, sondaki bosluk ve CR atilir. Cok satirli skaler SAYILI sinir.
    function yamlval(t, pos,    c, v) {
      YQ = 0; v = substr(t, pos)
      while (match(v, /^[&!][^ \t]*[ \t]+/)) v = substr(v, RLENGTH + 1)
      c = substr(v, 1, 1)
      if (c == SQ) { YQ = 1; return sqlstr(v, 2) }
      if (c == "\"") { YQ = 1; return dqstr(v, 2) }
      if (match(v, /[ \t]#/)) v = substr(v, 1, RSTART - 1)
      sub(/[ \t\r]+$/, "", v)
      return v
    }
    # yamlnoval: TIRNAKSIZ bir YAML degeri bu satirda deger TASIMIYOR — yorum (`#`),
    # blok skaler gostergesi (`|`, `>-`, `|+2`: deger alt satirlarda, sinir #6), takma
    # ad (`*ad`), ya da ardinda deger olmayan tek bir ozellik (`&ad`, `!!str`).
    function yamlnoval(v) {
      return (v ~ /^#/ || v ~ /^[|>][-+0-9]*$/ || v ~ /^[*]/ || v ~ /^[&!][^ \t]*$/)
    }
    # dqstr: cift tirnakla acilan dizgenin degeri; `\x` kacisi x olarak okunur.
    function dqstr(t, pos,    c, v) {
      v = ""
      while (pos <= length(t)) {
        c = substr(t, pos, 1)
        if (c == "\\") { v = v substr(t, pos + 1, 1); pos += 2; continue }
        if (c == "\"") break
        v = v c; pos++
      }
      return v
    }
    # sqlstr: tek tirnakla acilan SQL dizgesinin degeri; iki tek tirnak bir tirnaktir (kacis). Ayni
    # satirda bitisik ikinci bir dizge ya da alt satira devam SAYILI sinir.
    function sqlstr(t, pos,    c, v) {
      v = ""
      while (pos <= length(t)) {
        c = substr(t, pos, 1)
        if (c == SQ) { if (substr(t, pos + 1, 1) == SQ) { v = v SQ; pos += 2; continue } break }
        v = v c; pos++
      }
      return v
    }
    function classes2(v,    a) {
      a = v; gsub(/[^ -~]/, "", a)
      return (a ~ /[a-z]/) + (a ~ /[A-Z]/) + (a ~ /[0-9]/) + (a ~ /[^A-Za-z0-9]/)
    }
    # strongval: duz yazi/kod degil, bir DEGER gibi gorunen: >=6 karakter, >=2 sinif,
    # yer tutucu ve kod bicimi degil. (1b), (4) yeni anahtarlari ve (5) kullanir.
    function strongval(v) {
      if (placeholder(v, 0, SIG)) return 0
      if (hexsecret(v) || longalnum(v)) return 1
      return (length(v) >= 6 && classes2(v) >= 2 && !codeish(v))
    }
    # refish: TIRNAKSIZ bir atama degerinde deger degil BASVURU olan sekiller — secici
    # (`cfg.Password`: Python `password=settings.X`), ENV adi (`YOUR_API_KEY`), yol, URL.
    # Duz bir tanimlayici (`tappaZq9w`, `Summer2024`) burada DEGERDIR: atamada
    # codeish (tanimlayici = kod) kullanilmaz, cunku sag taraf zaten bir degerdir.
    # 5. tur N3: CAGRI sekli (`f(x)`) SILINDI — kabuk kelimesi ve .md/.toml degeri `(`
    # da zaten biter, cagriya yalniz YAML duz skaleri ulasiyordu ve YAML da cagri
    # yoktur (`tappa(Zq9w!x)` sessizdi, olculdu). Tirnakli degere hic uygulanmaz.
    function refish(v) {
      if (index(v, "://")) return 1
      if (v ~ /^[A-Za-z_][A-Za-z0-9_]*[.][A-Za-z_][A-Za-z0-9_.]*$/) return 1
      if (v ~ /^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$/) return 1
      return (v ~ /^(\/|[.]\/|[.][.]\/|~\/)/)
    }
    # assignval: bir ATAMANIN (X=, SQL dizgesi, YAML/TOML degeri, curl -u) degeri
    # yakalanacak kadar GUCLU mu — 4. tur karari: dev degerleri (tappa gibi 5
    # karakterlik tek sinifli) hic yakalanmaz, dolayisiyla muafiyet de gerekmez.
    # q=1: deger TIRNAKLIYDI — bir dizge sabiti cagri, secici ya da ENV adi olamaz
    # (5. tur N3), refish atlanir. lit: placeholder ile ayni.
    function assignval(v, q, lit) {
      if (placeholder(v, lit, SIG)) return 0
      if (hexsecret(v) || longalnum(v)) return 1
      return (length(v) >= 6 && classes2(v) >= 2 && (q || !refish(v)))
    }
    # curl_cred: `curl ... -u <kullanici>:<parola>`, `--user` ve `--user=` bicimleri.
    function curl_cred(t,    n, W, i, cr, k, q) {
      if (index(tolower(t), "curl") == 0) return 0
      n = split(t, W, /[ \t]+/)
      for (i = 1; i <= n; i++) {
        cr = ""
        if ((W[i] == "-u" || W[i] == "--user") && i < n) cr = W[i + 1]
        else if (substr(W[i], 1, 7) == "--user=") cr = substr(W[i], 8)
        if (cr == "") continue
        q = gsub(/["`]/, "", cr) + gsub(SQ, "", cr)
        k = index(cr, ":")
        if (k && assignval(substr(cr, k + 1), q > 0, 0)) return 1
      }
      return 0
    }
    function c_assign(t, path,    lt, s, off, k, v, j) {
      lt = tolower(t)
      # (1) KEY=V, anahtar password ailesiyle biter (PGPASSWORD=, --password=).
      s = lt; off = 0
      while (match(s, /(password|passwd|passphrase)=/)) {
        k = off + RSTART + RLENGTH
        v = shword(t, k)
        if (substr(t, k, 1) != "=" && assignval(v, SWQ, SWSQ)) return 1
        off = k - 1; s = substr(lt, off + 1)
      }
      # (1b) 2. tur: yeni anahtarlar YALNIZ tam bir `_`/`-` parcasi olarak (db_pass=
      # evet, bypass= hayir). Deger GUCLU olmali (assignval) — `local pass=0`,
      # `PASS=false`, `?user=a&pass=1`, `api_key=None` kod/duz metindir.
      s = lt; off = 0
      while (match(s, /(^|[^a-z0-9_-])-*([a-z0-9]+[_-])*(pass|pwd|apikey|api[_-]key|secret[_-]key|access[_-]key|auth[_-]token|(kek|hmac[_-]key)([_-](previous|prev|old|new|next|current|b64|base64|hex|raw|v?[0-9]+))*)=/)) {
        k = off + RSTART + RLENGTH
        v = shword(t, k)
        if (substr(t, k, 1) != "=" && assignval(v, SWQ, SWSQ)) return 1
        off = k - 1; s = substr(lt, off + 1)
      }
      # (2) SQL: PASSWORD <tek tirnakli deger> ve IDENTIFIED BY <tek tirnakli deger>.
      # Dizge her zaman tirnaklidir. .sql dosyasinda `$` acilmaz (psql degiskeni
      # `:ad` bicimindedir); baska dosyada SQL cogu kez cift tirnakli bir kabuk
      # dizgesi ya da tirnaksiz heredoc icindedir ve orada `$X` ACILIR.
      s = lt; off = 0
      while (match(s, /(password|identified[ \t]+by)[ \t]+/)) {
        k = off + RSTART + RLENGTH
        if (substr(t, k, 1) == SQ && assignval(sqlstr(t, k + 1), 1, path ~ /[.]sql$/)) return 1
        off = k - 1; s = substr(lt, off + 1)
      }
      # (3) curl -u <kullanici>:<parola>.
      if (curl_cred(t)) return 1
      # (4) YAML satir basi `anahtar: deger`; anahtar `-`/`_` ayracli, cift tirnakli
      # (`"password":`) ya da bir liste ogesi (`- password:`) olabilir. YAML tek
      # tirnagi `$` i acmaz AMA Compose `${X}` i tirnakli degerde de acar: lit=0.
      if (path ~ /[.]ya?ml$/ && match(lt, /^[ \t-]*"?[a-z0-9_-]*(password|passwd|passphrase)"?[ \t]*:[ \t]+/)) {
        v = yamlval(t, RSTART + RLENGTH)
        if ((YQ || !yamlnoval(v)) && assignval(v, YQ, 0)) return 1
      }
      if (path ~ /[.]ya?ml$/ && match(lt, /^[ \t-]*"?([a-z0-9]+[_-])*(pass|pwd|apikey|api[_-]key|secret[_-]key|access[_-]key|auth[_-]token|(kek|hmac[_-]key)([_-](previous|prev|old|new|next|current|b64|base64|hex|raw|v?[0-9]+))*)"?[ \t]*:[ \t]+/)) {
        v = yamlval(t, RSTART + RLENGTH)
        if ((YQ || !yamlnoval(v)) && assignval(v, YQ, 0)) return 1
      }
      # (5) Belge/yapilandirma: `anahtar: deger` / `anahtar = deger`, satirin herhangi
      # bir yerinde. .toml/.ini/.cfg/.conf/.properties dosyalarinda ve .md icinde
      # TIRNAKLI deger bir degerdir (assignval); .md icindeki TIRNAKSIZ deger duz yazi
      # olabilir ("password: required"), orada strongval (kod bicimi de dislanir).
      if (path ~ /[.](md|toml|ini|cfg|conf|properties)$/) {
        s = lt; off = 0
        while (match(s, /(^|[^a-z0-9_-])([a-z0-9]+[_-])*(password|passwd|passphrase|pass|pwd|apikey|api[_-]key|secret[_-]key|access[_-]key|auth[_-]token|(kek|hmac[_-]key)([_-](previous|prev|old|new|next|current|b64|base64|hex|raw|v?[0-9]+))*)"?[ \t]*(:|=)[ \t]*/)) {
          k = off + RSTART + RLENGTH
          v = val_at(t, k)
          if ((path !~ /[.]md$/ || VQ) ? assignval(v, VQ, 0) : strongval(v)) return 1
          off = k - 1; s = substr(lt, off + 1)
        }
      }
      return 0
    }
    function wordlike(w) { return (w ~ /^[a-z0-9]+$/ || w ~ /^[A-Z][a-z0-9]*$/ || w ~ /^[A-Z0-9]+$/) }
    function kebab(s,    n, k, P) {
      sub(/^[.]/, "", s)
      if (s !~ /^[A-Za-z0-9]+(-[A-Za-z0-9]+)+$/) return 0
      n = split(s, P, "-")
      for (k = 1; k <= n; k++) if (!wordlike(P[k])) return 0
      return 1
    }
    # pathish: yol karakterleriyle en az bir `/`. AMA base64 de `/` tasir, bu yuzden
    # BUYUK HARF iceren bir aday ancak bir YOL IPUCU da tasiyorsa yol sayilir: `/`,
    # `./`, `~/` ile baslar, `:` icerir (imaj:etiket, k:v/k:v), bir parcasi dosya
    # uzantisiyla biter (ev2.go/...) ya da HIC RAKAM icermez (Role/github-deployer,
    # Europe/Malta). Olculdu: ipucusuz haliyle `+`/`=` icermeyen 32 karakterlik bir
    # base64 degeri YOL diye muaf kaliyordu; 32 karakterlik rastgele base64 yuzde
    # 99,6 olasilikla en az bir rakam tasir.
    function pathish(s) {
      if (s !~ /^[A-Za-z0-9_.*@:~,{}|%-]*(\/[A-Za-z0-9_.*@:~,{}|%-]*)+([?][A-Za-z0-9_=&%.-]*)?$/) return 0
      if (s !~ /[A-Z]/ || s !~ /[0-9]/) return 1
      return (s ~ /^(\/|[.][.]?\/|~\/)/ || index(s, ":") || s ~ /[.][a-z][a-z0-9]*([\/?]|$)/)
    }
    # codeish: agacta OLCULMUS yanlis pozitif sekilleri. Her satirin karsisinda
    # onu gerektiren gercek bir ornek var (yorumda, kesme isaretsiz).
    function codeish(s) {
      if (s ~ /^\.?[A-Za-z_*][A-Za-z0-9_.*]*$/) return 1                        # ident, pkg.Func, .Field, store.*Params.X, Resource*
      if (s ~ /^[A-Za-z_][A-Za-z0-9_.*]*[({[]/) return 1                        # f(x), T{}, x.(*T), a[0]
      if (s ~ /^[A-Za-z_][A-Za-z0-9_.-]*(==?|:)[]A-Za-z0-9_.,\/:{}()*+[-]*$/) return 1 # SameSite=Lax, iterations:1, resourceNames:[x]
      if (s ~ /^[A-Za-z_][A-Za-z0-9_.-]*(==?|:)([-_.,\/:@]*(%[-+ #0]*[0-9*]*([.][0-9*]+)?[a-zA-Z]|%[(][A-Za-z_][A-Za-z0-9_]*[)][a-zA-Z]))+[-_.,\/:@]*$/) return 1 # PGPASSWORD=%-20s, user=%s:%d — YALNIZ fiil + alfanumerik OLMAYAN ayrac (9. tur)
      if (s ~ /^[A-Za-z0-9_.-]+[.][A-Za-z][A-Za-z0-9]*(:[0-9]+([-\/,][0-9]+)*:?)?$/) return 1 # 20-app.yaml, ev2.go:348/353, .env.example:2: (grep bicimi)
      if (pathish(s)) return 1                                                   # yol, imaj:etiket, /activate?x=1
      if (s ~ /^[$]([A-Za-z_][A-Za-z0-9_]*|[{][^}]*[}])\// && pathish(substr(s, index(s, "/")))) return 1 # $HOME/.kube/config (fd56c28 deploy.yml; 6. tur duz baglam)
      if (kebab(s)) return 1                                                     # t73-admin-change-password, Set-Cookie, .min-h-11
      if (s ~ /^[A-Za-z_][A-Za-z0-9_.]*[-+*\/][0-9]+$/) return 1                # MinPasswordRunes-1
      if (s ~ /^[A-Za-z0-9_]+(,[A-Za-z0-9_]+)+$/) return 1                      # MacBookPro16,1
      if (s ~ /^[A-Za-z]*[0-9]+([.][0-9]+)+[A-Za-z0-9.-]*$/) return 1           # go1.26.7
      if (s ~ /^[0-9][0-9:T.+-]*Z?$/) return 1                                   # 2026-08-15T20:15:45Z, 08:38:41Z
      if (s ~ /^\[[0-9]*\]/) return 1                                            # [44]byte
      if (s ~ /^[0-9]*[<>]+&?[A-Za-z0-9_.\/-]*$/) return 1                       # 2>/dev/null, 2>&1 (gecmiste, backlog a7330ae)
      if (s ~ /^[A-Za-z0-9._%+-]*@[A-Za-z0-9-]+[.][A-Za-z0-9.-]+$/) return 1     # @m6.example, e-posta
      return 0
    }
    # hexsecret: IKI SINIFLI oldugu icin sinif sayimindan gecemeyen ama `openssl rand
    # -hex N` ile uretilen parolanin (ve cogu API anahtarinin) tam sekli. >=32 hane =
    # >=128 bit; plaket UID (14), CMAC (16) ve kisa commit sha altinda kalir. Tek
    # karakter tekrari ve 0123456789abcdef dizisi dolgudur, sir degil — agacta sir
    # kelimesiyle ayni satirdaki IKI aday da tam olarak bunlardi (olculdu).
    function hexsecret(s,    t) {
      if (length(s) < 32 || s !~ /^[0-9a-fA-F]+$/) return 0
      t = s; gsub(substr(s, 1, 1), "", t)
      if (t == "" || index(tolower(s), "0123456789abcdef")) return 0
      return 1
    }
    # longalnum (3. tur, a): >=32 harf+rakam, buyuk+kucuk+rakam — bir SES SMTP parolasi
    # ya da `+`/`/` tasimayan base64 bir anahtar TANIMLAYICI bicimindedir ve codeish onu
    # kod sanardi. Ilk 8 karakteri ilerde yeniden gecen (FAKEfakeFAKE..., RESETreset...)
    # dolgudur. Olculdu: agacta sir kelimeli satirda 7 aday; 5 dolgu, 2 sentetik test
    # degeri (tabloda adiyla). `_`/`-` tasiyan base64url DISARIDA: Go test adlari
    # (TestEV2_..., TestADR0005_...) ayni bicimde (olculdu) — sayili sinir #3.
    function longalnum(s) {
      if (length(s) < 32 || s !~ /^[A-Za-z0-9]+$/) return 0
      if (s !~ /[A-Z]/ || s !~ /[a-z]/ || s !~ /[0-9]/) return 0
      return (index(substr(s, 9), substr(s, 1, 8)) == 0)
    }
    function secretish(s) {
      if (length(s) < 8 || length(s) > 128 || s ~ /[ \t]/) return 0
      if (hexsecret(s) || longalnum(s)) return 1
      if (index(s, "\"") || index(s, SQ) || index(s, "\\")) return 0            # ic ice tirnak, kacis, regex
      if (index(s, "<") && index(s, ">")) return 0                               # <yer tutucu>
      if (index(s, "…") || index(s, "...") || index(s, "://")) return 0         # kisaltma; URL url-cred sinifinin
      if (s ~ /^[$%]/ && sigilref(s, SIG)) return 0                              # $X, ${X}, %s (5.-7. tur: sigilref)
      if (s ~ /^\^/ && (s ~ /[$]$/ || s ~ /[(|*+?{]/ || index(s, "["))) return 0 # ^regex$ (5. tur: yalniz regex bicimi)
      if (s ~ /\[[A-Za-z0-9]-[A-Za-z0-9]/) return 0                              # [a-z]
      return (classes2(s) >= 3 && !codeish(s))                                   # ASCII disi baytlar sembol sayilmaz
    }
    # keyshaped (11. tur): bir ANAHTARIN tam bicimi — >=32 hex (hexsecret), >=32 harf+
    # rakam (longalnum), ya da 32 ya da 16 bayta cozulen base64 (standart ya da url
    # alfabesi; 44/24 karakter `=`/`==` dolgulu, 43/22 dolgusuz). base64 adayinda buyuk,
    # kucuk harf ve rakam birlikte ve ilk 8 karakter yeniden gecmiyor (dolgu degil).
    function keyshaped(s,    L) {
      # 40 hex = bir git nesne adi (SHA-1): `{Key: "vcs.revision", Value: "<sha>"}`
      # gibi satirlar anahtar degildir (olculdu); AES/HMAC anahtari 32/48/64 hanedir.
      if (hexsecret(s)) return (length(s) != 40)
      if (longalnum(s)) return 1
      L = length(s)
      if (!((L == 44 && s ~ /^[A-Za-z0-9+\/_-]+=$/) || (L == 24 && s ~ /^[A-Za-z0-9+\/_-]+==$/) ||
            ((L == 43 || L == 22) && s ~ /^[A-Za-z0-9+\/_-]+$/))) return 0
      if (s !~ /[A-Z]/ || s !~ /[a-z]/ || s !~ /[0-9]/) return 0
      return (index(substr(s, 9), substr(s, 1, 8)) == 0)
    }
    # c_a0: sir kelimesi (TRIG) + a0 adayi (secretish), ya da 11. turdan beri ANAHTAR
    # baglami (KEYCTX) + anahtar bicimli aday (keyshaped). Ikisi de tirnakli adaya bakar.
    function c_a0(t,    k, q, s, i, r, j, lt, tr, kc, cand) {
      lt = tolower(t); tr = (lt ~ TRIG); kc = (lt ~ KEYCTX || t ~ KEYCAMEL)
      if (!tr && !kc) return 0
      for (k = 1; k <= 3; k++) {
        q = Q[k]; s = t
        while ((i = index(s, q)) > 0) {
          r = substr(s, i + 1); j = index(r, q)
          if (j == 0) break
          cand = substr(r, 1, j - 1)
          if ((tr && secretish(cand)) || (kc && keyshaped(cand))) return 1
          s = substr(r, j + 1)
        }
      }
      return 0
    }
    function classes(t, path,    c) {
      c = ""
      if (c_url(t)) c = add(c, "url-cred")
      if (c_aws(t)) c = add(c, "aws-key")
      if (c_pem(t)) c = add(c, "pem-key")
      if (c_bcrypt(t)) c = add(c, "bcrypt")
      if (c_known(t)) c = add(c, "known-token")
      if (c_assign(t, path)) c = add(c, "pw-assign")
      if (c_a0(t)) c = add(c, "a0-token")
      return c
    }
    {
      # Kayit: yol US satir US metin. rg ikili dosya icin kendi bildirimini basar
      # (`yol: binary file matches (found ... byte around offset N)`) — beklenen tek
      # US siz satir. Baska her ayrisamayan satir SAYILIR ve sonda exit 2 olur.
      i = index($0, SEP)
      if (i == 0) {
        if ($0 == "" || $0 ~ /: binary file matches \(found .* byte around offset [0-9]+\)$/) next
        bad++; next
      }
      p = substr($0, 1, i - 1); r = substr($0, i + 1); j = index(r, SEP)
      if (j == 0 || substr(r, 1, j - 1) !~ /^[0-9]+$/) { bad++; next }
      no = substr(r, 1, j - 1); t = substr(r, j + 1)
      # count/pick: yolu bir girdiye uyan kayit sayilir; pick onun TAM metnini (satir
      # sonu haric, `\r` ve sondaki bosluk DAHIL) kendi dosyasina yazar.
      if (mode == "count") { if (onpath(p)) npath++; next }
      if (mode == "pick") {
        if (onpath(p)) { f = dir "/c." NR; printf "%s", t > f; close(f) }
        next
      }
      # 2. gecis: hash tutan satir HIC siniflanmaz; tutmayan, adsiz bir yoldaki gibi.
      if ((NR in H) && waived(p, H[NR])) {
        if (mode == "waived") print p ":" no ": [muaf]"
        next
      }
      if (mode != "fail") next
      SIG = sigmode(p)
      c = classes(t, p)
      if (c != "") print p ":" no ": [" c "]"
    }
    END {
      if (bad) {
        printf "secretscan: %d kayit ayristirilamadi (yol US satir US metin bekleniyordu) — TARAMA GUVENILIR DEGIL\n", bad > "/dev/stderr"
        exit 2
      }
      if (mode == "count") print npath
    }'
}

# Dogrudan calistirilirsa (kaynak edilince DEGIL): muafiyet satirinin basini uretir.
#   bash scripts/secretscan.sh --hash <depo-goreli dosya>:<satir no>
# Satirin METNINI basmaz; yalniz `^<yol>$;<sha256>;` ve aciklama yer tutucusunu.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  if [[ $# -ne 2 || $1 != --hash || $2 != *:* ]]; then
    echo "kullanim: bash scripts/secretscan.sh --hash <depo-goreli dosya>:<satir no>" >&2
    exit 2
  fi
  r7d_f=${2%:*}; r7d_n=${2##*:}; r7d_f=${r7d_f#./}
  if [[ ! -f $r7d_f || $r7d_f == /* || ! $r7d_n =~ ^[1-9][0-9]*$ || ! $r7d_f =~ ^[A-Za-z0-9_./@-]+$ ]]; then
    echo "secretscan: depo-goreli bir dosya ve 1'den baslayan satir no gerekli (yolda yalniz [A-Za-z0-9_./@-])" >&2
    exit 2
  fi
  if (( r7d_n > $(LC_ALL=C awk 'END { print NR }' "$r7d_f") )); then
    echo "secretscan: $r7d_f o kadar satir tasimiyor" >&2
    exit 2
  fi
  r7d_hasher || exit 2
  # shellcheck disable=SC2086 # R7D_HASHER bilerek bolunur
  r7d_h=$(LC_ALL=C awk -v n="$r7d_n" 'NR == n { printf "%s", $0; exit }' "$r7d_f" | $R7D_HASHER) || exit 2
  printf '^%s$;%s;<aciklama: deger ve sir kelimesi YOK>\n' "${r7d_f//./[.]}" "${r7d_h:0:64}"
fi
