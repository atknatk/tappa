# Durum

> **Bu dosya projenin tek canlı durum kaynağıdır.** Her oturumun sonunda
> güncellenir ([README.md](README.md) → oturum protokolü, adım 6.4).
> Görev kartlarına durum işareti konmaz.

**Son güncelleme:** 2026-08-25 (17. oturum, üçüncü yarı — 🟢 **PUSH + DEPLOY YAPILDI: CANLI `af8b44c`, `goose … version: 22`, §4.5 kapısı geçti** — yani **encode uç noktası ve `internal/sun`'ın wipe disiplini ÜRETİMDE** · 🔴 **AMA PUSH BİR ŞEY ORTAYA ÇIKARDI: main 2026-08-22'DEN BERİ KIRMIZIYDI**, aynı testle (`TestAuditIndex00021`), yani **üç gündür hiçbir şey deploy edilmiyordu ve fark edilmemişti** · kusur **testindi, planlayıcının değil**: test indeksin **tercih edilmesini** bekliyordu, CI'da tablo küçük olduğu için **Seq Scan ucuz** ve planlayıcı **doğru** olanı yapıyordu; yerelde yeşildi çünkü dev DB **~279.000 satır** · 🔴 **testin *"5 satır yeter"* ölçümü bir ARTEFAKTMIŞ — geri alınan sondalar bile ölü heap sayfaları bırakıyor, süpürme tabloyu KENDİ AYAĞININ ALTINDA şişiriyor**; gerçek geçiş **~250 satır**, fixture **%20 altında** · **(A) elendi: eşik `random_page_cost` 1.1↔40 arasında 25 KAT kayıyor, testin seçebileceği bir sayı değil** · ✅ çare yine **soruyu değiştirmek** oldu (*"seçecek mi"* → *"kullanabilir mi"*), ve **yapıcı benim önerimi ölçümle çürüttü**: ANALYZE olmadan planlayıcı **yanlış indeksi** seçiyor · 🔴 **daralma dürüstçe yazıldı ve backlog T17'ye kaydedildi: T17'nin asıl korkusunu artık HİÇBİR KAPI korumuyor** · **SIRADAKİ: ledger'da bloke olmayan görev yok; hijyende T65 (*"M8 pilot öncesi"*)** — *(ikinci yarı: ✅ **BACKLOG T64 DONE** (`f540f8a`): `internal/sun/cmac.go` **hiçbir ara tamponu silmiyordu** ve bu **SUN doğrulaması dahil her MAC yolundaydı**; **34 tampon** deferred-wiped, kapı **üç dosyada 146 bildirimi** sınıflandırıyor · 🔴 **en ağırı CBC biriktiricisiydi — dönüşte `KSesAuthENC`/`KSesAuthMAC`'in birebir kendisi, ve HEAP'te** · 🔴 **bir muafiyet gerekçesi tersini söylüyordu:** `mac` ve `want` **karşılaştırma** değerleridir, ve **reddedilen** bir tap'te sayaç ilerlemediği için kalan `mac` saldırganın **sahip olmadığı** doğru MAC · 🔴 **kapı DÖRT KEZ yenildi** (`x[:0]` · ulaşılmayan `defer` · `else if`'e kör yürüyüş · **kardeş bloklar**) ve çare bir **ÖLÇÜT** oldu: *"kazayla yazılanı KAPAT, kasıtlı kaçamağı SAY — bir kapı bir güvenlik sınırı değildir"* · 🔴 **güvenlik merceği yine doğruluk merceğinin göremediğini buldu:** `cipher.NewGCM` `Block`'u **değerle** gömüyor → **her tap ~1 KB referanssız heap bırakıyor, içinde parkın tüm tag anahtarlarını açan KEK** — **kapatılamaz, SAYILDI** · ⚠️ **KAT'lar bu sınıfa KÖR (ölçüldü)** · ⚠️ **iki süreç kusuru: benim mührüm izlenmeyen dosyayı görmüyordu; yapıcının harness'ı üç kez artık bıraktı** · 🔴 **VE LEDGER'DA BLOKE OLMAYAN GÖREV KALMADI** — B3 (Android zinciri **bu makinede yok** + donanım + md.5 + Q08) · M8-06 (Q13+T45) · M8-07 · M9 (MVP dışı/Q22) · **SIRADAKİ: kullanıcı kararı**)* — *(önceki yarı: 🔴 **M8-05 FAZ B2c-2b DONE: ENCODE UÇ NOKTASI** (`f632f09`) · üç rota `ProtectWriting` + encode bütçesinin **iç grubunda**, **tenant ve aktör OTURUMDAN** (denetçiler **on beş taşıyıcı** denedi, hiçbiri geçmedi), `internal/encode` **artık üretimden çağrılıyor** · ✅ **ADR 0017 §6 md.10 · md.12 · devir md.18 KAPANDI** · 🔴 **11 yapıcı turu / 11 bağımsız denetim (9 üçüncü göz + 2 güvenlik), 10'u RED** · 🔴 **GÖVDE KAPISI ÜÇ TASARIM KAYBETTİ VE SONRA SAYILDI** (`body any` → anonim struct · mühürlü arayüz → **struct GÖMMESİ** · değer parametresinin kaldırılması → **muaf yazıcının `w` yetkisi**); üçünde de sızıntı **gerçek router'dan** ölçüldü ve **paket yeşil kaldı** — sınıf B2c-2a'nın §4.7 duvarıyla **aynı**, *"bir sızıntı nasıl görünür"* sorusu **kompozisyonla sonsuz**; **dördüncüsü denenmedi** · 🔴 **VE 2. GÜVENLİK GEÇİŞİ, BEŞ GENEL DENETÇİNİN GÖREMEDİĞİ GERÇEK BİR ÜRÜN KUSURU BULDU:** `plaque.unmarked`, kodun **kendisinin *"OLAĞAN"* dediği** tetikleyicide (telefon kapanır → istek ctx'i ölür) **hiç yazılmıyordu**; çare **iki katmanlı** oldu — olağan kapanma artık **ONARILIYOR** (`markEncoded` de kopuk context'te), telafi girdisi ise **DB gerçekten erişilemezken** yazılıyor · 🔴 **VE ASIL DERS: BİR SAYIM ON BİR TURDA ON BİR KEZ EKSİK ÇIKTI**, iki kez **düzeltmenin kendisi** yenisini üretti; çare liste değil **ÖZELLİK** oldu (*"telafi rapor ettiği yolu kat eder"* → **satır varsa DB erişilebilirdi, satır yoksa hiçbir şey kanıtlanmaz**) · `audit_log.actor_id` artık **gerçek admin id'si** (md.8'in kararı değişti, ADR'de **tarihsel damgayla**) · **SIRADAKİ: M8-05 FAZ B3 — Android rölesi, M8'İN TEK DONANIMA BLOKE FAZI**; devir listesi **28 madde, 5 kapandı / 23 açık**, ve **md.16 hepsini yönetiyor: hiçbir çip encode edilmedi**)*)

> *(16. oturum: 🔴 **M8-05 FAZ B2c-2a DONE: VERİ KATMANI** (`b5f4390`) · migration **00022** (`encoded_at` + **iki trigger**), `tags`'e **projenin ilk INSERT'i**, `Rows`/`Wrapper`, **`audit_log`'a iki olay**, **`tenant_id` açık parametre** · 🔴 **10 yapıcı turu / 9 bağımsız denetim, 32 bloklayan** · ✅ **T16'nın SELECT yarısı KAPANDI** (`REVOKE SELECT ON tags` → **`aes_key_ref` on sütunun tek okunamayanı**; iki denetçi **41 doğrudan şekil** attı, hepsi `permission denied`) · 🔴 **VE BU TURUN DERSİ BİR YÖN DEĞİŞİKLİĞİ:** §4.7 duvarı **beş metin/AST tasarımı** denedi, **beşi de** bir sonraki denetimin bulduğu bir yazıma yenildi (kasa katlaması · bütün-satır fonksiyonları · Unicode escape · `[][]byte` · alias · `sqlc.yaml` override) — çünkü *"bir sızıntı nasıl görünür"* sorusu **sonsuz**; **ayrıcalık o soruyu hiç sormuyor** · 🔴 **kapanmayan ADIYLA SAYILDI** (durma kuralı 2): anahtar `resolve_tag_by_uid` üzerinden okunabilir ve **kapatılamaz** (tap tenant bağlamı olmadan gelir), **yürürlükte mekanizma yok ama MEVCUT OLAN VAR ve alınmadı** (ayrı çözümleme rolü), envanter **dört kaçamaklı yazımı yakalamıyor**, **ve o yol tenant sınırını da aşıyor** — **devir listesi md. 19** · ⚠️ ~~**`internal/encode` hâlâ hiçbir şeyden import edilmiyor**~~ — **B2c-2b'de kapandı** · **SIRADAKİ: M8-05 FAZ B2c-2b — HTTP rölesi ve yetkilendirme, DONANIM GEREKMEZ**)*

> *(15. oturum, altıncı yarı: 🔴 **M8-05 FAZ B2c-1 DONE: OTURUM DEPOSU VE TUR SÜRÜCÜSÜ** (`6bb6cdf`) · `internal/encode`, **encode aracının ÖMÜR yarısı** · 🔴 **BU PROJENİN EN UZUN DENETİM ZİNCİRİ: 12 yapıcı turu / 10 bağımsız denetim (8 üçüncü göz + 2 güvenlik), 42 bloklayan** · kapsam **%92,6** (127 vaka) · **76 mutasyon, 67 kırmızı, 9 belgeli hayatta kalan** · ✅ **§6 md.7 KAPANDI** (TTL 90 sn · iki süpürücü · 1/3/64 · silme garantisi **yerden bağımsız** + **sayılmış açık**) · ✅ **`GetCardUID` kapısı adım 4b'ye taşındı** — ilk geri döndürülemez komuttan **önce**; tespit gücü aynı (adım 5–8 tek anahtar-0 oturumu), ama sonuç **kalıcı plaket kaybı**ndan **hayalet satır**a indi · 🔴 **üç GERÇEK üretim kusuru**: plaket anahtarının rastgeleliği **ölçülmüyordu** · `Close` meşgul oturumu silip `aes_key_ref`'e **16 sıfır bayt** yazdırıyordu · context son alışverişte ölünce **kişiselleştirilmiş** çip `Done:false` dönüyordu · 🔴 **ama baskın sınıf KOD DEĞİL KANITTI:** kapalı sayım **9 kez**, testin yorumu-gövde uyuşmazlığı **6 kez**, *"yapmadan yapıldı demek"* **3 kez**, ve **bir düzeltmenin çürüttüğü komşu cümleler 5 kez** — son ikisini **orkestratör kendi doğrulamasında** buldu · **son üç denetim üretim baytlarında kusur BULAMADI** · ⚠️ **araç kazası: paylaşılan scratchpad'de sabit adlı bir betik üretim kodunu iki kez sessizce geri aldı** · **SIRADAKİ: M8-05 FAZ B2c-2 — HTTP rölesi ve kalıcılık, DONANIM GEREKMEZ** — *B2c-2 sonradan **B2c-2a/B2c-2b** olarak bölündü; B2c-2a bitti*)*

> *(15. oturum, beşinci yarı: 🔴 **M8-05 FAZ B2b DONE: KOMUT KATMANI** (`c170129`) · **yedi komut**, encode aracının **bayt üreten yarısı TAMAM** · **4 yapıcı turu / 3 bağımsız denetim, 3 bloklayan** · kapsam **%96,7** · **40 mutasyon, 39 kırmızı** ve **ilk geçişte hayatta kalan üçün üçü de GERÇEK boşluktu** · 🔴 **M2-08 riski vektörsüz çözüldü**: aynı kurucu **iki yapılandırmayı da** üretiyor ve **yayımlanmışı yeniden üretiyor**; **iki ofset kopyalanmadı, ÖLÇÜLDÜ** · 🔴 **`FileAR` KARARI, §6 md.13 KAPANDI** — ama **niteleyiciler daralmadan önemli**: oltalama **yalnız** halka açık anahtar 0'lı tarafa karşı, **yalnız** adım 7'den sonra, **yalnız** dosya `02h` için kapandı · 🔴 **ve md.13'ün KAPSAMADIĞI iki yol bulundu**: **Yetenek Konteyneri** (AN12196 tam C-APDU'yu **yayımlıyor**) ve **`Change = Fh` GERİ ALINAMAZ** (format komutu **yok**) · **Q08 kısıtlanmıyor** (69 baytlık bütçe, `tappa.mt/t` = 69) · ⚠️ **süreç hatası: yapıcı iki turdur *"PDF'i ölçemem"* demiş, hiç denememişti** · **SIRADAKİ: M8-05 FAZ B2c — durumlu oturum ve HTTP rölesi, DONANIM GEREKMEZ** — *B2c sonradan **B2c-1/B2c-2** olarak bölündü; B2c-1 bitti*)*
>
> *(15. oturum, dördüncü yarı: 🔴 **M8-05 FAZ B2a DONE: EV2 KRİPTO ÇEKİRDEĞİ** (`c944431`), **ürünün ilk encode kodu** · **4 yapıcı turu / 3 bağımsız denetim**, iki mercek de ONAY · **kapsam %95,9** · **11/11 bayt-sırası mutasyonu kırmızı** · 🔴 **XOR artık tekrarlanmış bir DENEY**: silinince **tam bir** test kırmızı, **29 yayımlanmış vektör yeşil** · 🔴 **belgenin iki iç çelişkisi ÖLÇÜMLE çözüldü** · 🔴 **bloklayan sabit-zaman envanteriydi** — M2-08 için yazılmış ratchet, yeni kripto **kaydolmadan geçmişti** · **`Zero` listesi artık MEKANİZMA** (güvenlik merceği on birini de sildi, suite **yeşil kaldı**) ve **hemen gerçek bir eksiği yakaladı** · yeni backlog **T64** (`cmac.go` hiçbir tamponu silmiyor, **SUN doğrulaması dahil her yolda**) — ✅ **2026-08-25'te KAPANDI, `f540f8a`** · **SIRADAKİ: M8-05 FAZ B2b — komut katmanı ve durumlu oturum, DONANIM GEREKMEZ** — *B2b sonradan **B2b/B2c** olarak bölündü; B2b bitti*)*
>
> *(15. oturum, üçüncü yarı: 🔴 **M8-05 FAZ B1 DONE: ADR 0017**, kod yok, **9 yapıcı turu / 8 bağımsız denetim, 7'si RED + 8'incisi ONAY, 24 bloklayan** · **kullanıcı ENCODE ARACINI SEÇTİ: kendi Android app'imiz**, mimari **APDU rölesi** → **ADR 0003 md.5 tadil edilmedi** · **`K_SDMFileRead` = `0x01`** · **anahtar 0 kişiselleştirilir, EN SON** ve sevkiyat bir **şema kararına** bloke · encode satırını **`tappa_app`** yazar → **T16'nın çözümü uygulanamaz** · **ADR 0005 altıdan SEKİZ riske**, sayı artık **bağlı** · **pilot kapısı altıdan YEDİ maddeye** · yeni backlog **T60·T61·T62** · ⚠️ **`make audit` sağlık satırı `:7` ile çelişiyordu, düzeltildi** · **SIRADAKİ: M8-05 FAZ B2 — sunucu tarafı, DONANIM GEREKMEZ** — *B2 sonradan **B2a/B2b** olarak bölündü; B2a bitti*)*
>
> *(15. oturum, ilk iki yarı: 🔴 **M8-04 DONE** (`239d427` + `ac29cf7` + `73dd3b9`; 12 yapici turu / **25 bagimsiz denetim, 20'si RED**) · **T31 KAPANDI**, `go1.26.7`, **`make audit` exit 0** · ⚠️ **BIR ORKESTRATOR HATASI:** B3'un uygulamasi, SIGTERM'lenen bir ajan *"reddedildi"* diye okundugu icin **denetlenmeden** `ac29cf7`'ye girdi ve push edildi; sonradan denetlendi (5 tur, 4 RED) · ⚠️ **kriter 4 kismen**: gercek etiketle replay **donanima bloke** · ⚠️ **orkestrator borcu: FAZ A'nin F7 metni repoda yok** · yeni backlog **T57·T58·T59** · **M8'in kalan uc gorevi de KULLANICIYA BLOKE** — 🔴 **bu son cümle 2026-08-20'de öldü: M8-05'in aracı seçildi, FAZ B2 bir GÖREV**)*
>
> *(FAZ B2: `239d427`, 6 yapici turu / 10 denetim, 8'i RED — 🔴 **urun RLS'i tamamen olduren bir DSN ile itirazsiz aciliyordu** (`tappa_app` 1 satir ↔ `tappa_owner` 311.129) · 🔴 **iki kirmizi cizginin hicbir mekanik korumasi yoktu** (`NO FORCE RLS` sahibe 404.335 satir aciyordu ve suit **tamamen yesil** kaliyordu; owner'in view'i RLS'i definer olarak degerlendiriyor) · 🔴 **§5 okuma tarafi kapatildi** — dort satir zaten yanlis `ip_match=TRUE` tasiyor ve `transactions` **degismez** · **FAZ B3 o an kalmisti**)*
>
> *(14. oturum: 🔴 **SEVK EDILMIS KRIPTO KUSURU BULUNDU VE DUZELTILDI: M2-08** (`sv2()` SDM sayacini SV2'ye YANLIS SIRADA yaziyordu; encode edilen ilk cipin **ilk 255 tap'inin hepsi** reddedilirdi) · **M8-05 A/B'ye bolundu, FAZ A DONE** (**Q10 karara baglandi**) · **M8-03 DONE** (`624c555`+`ae07e7c`, 6 tur/3 RED — 🔴 **karar motoru hic verdict loglamiyordu**, ve kart `X-Request-Id` uzerinden **kendi saldiri yuzeyini** acmisti) · **Q28 acildi** (uyari hedefi + log saklama) · siradaki **M8-04**, ve **kullanicida iki karar var: encode araci + yazici donanimi**)*

> ⚠️ **BU DOSYADAKI ESKI BLOKLARI OKURKEN:** asagidaki 2026-08-16 bloklari **yazildiklari anin** durumunu anlatiyor ve bir kismi sonraki fazlarda **curutuldu**. **Bugunku durum icin YALNIZCA ledger satirina bak** (`M8-02` satiri) ve bu blogun hemen altindaki 2026-08-17 blogunu oku. 🔴 **2026-08-17'de olen uc cumle:** *"uc kullanici eylemi deploy'u tamamen blokluyor"* (DOCKERHUB_USERNAME/DOCKERHUB_TOKEN **yazildi**, `atknatk/tappa` ve `atknatk/tappa-migrate` **Public olarak yaratildi ve anonim gorunur olcuLdu**) · *"KEK dondurme araci repoda yok"* (**var**: `cmd/rotatekek` + `scripts/rotate-kek.sh`) · *"Rewrap/Rotate yok"* (**UnwrapAny var**).


> **M8-04 FAZ B2 DONE — 2026-08-20 (15. oturum), `239d427`, 6 yapıcı turu / 10 denetim (8'i RED).** Genel üçüncü göz **beş tur** (beş farklı ajan; 4. turda **ONAY**) + `tappa-security-auditor` **beş geçiş** (sonuncusu **ONAY**). Migration YOK, yeni bağımlılık YOK. **M8-04 hâlâ `wip`: FAZ B3 kaldı.**
>
> 🔴 **ÜRÜN, RLS'İ TAMAMEN ÖLDÜREN BİR DSN İLE İTİRAZSIZ AÇILIYORDU.** `internal/config` yalnız **eşitliği** reddediyordu; `DATABASE_MIGRATE_URL` **unset** bırakılıp `DATABASE_URL`'e owner DSN'i yazılınca kontrol atlanıyor ve sunucu açılıyordu — `healthz=200`, rol hakkında **sıfır uyarı**. Ölçüldü: aynı tenant bağlamında `tappa_app` **1** satır, `tappa_owner` **311.129**. Artık `db.New` **dört olgu** okuyor (`rolsuper` · `rolbypassrls` · RLS'li bir tablonun **sahibi** · o rolün **üyesi**) ve üretimde **reddediyor**; reddedilen havuz **kapatılıyor ve hiç döndürülmüyor**. ⚠️ **Kapı `main`'e değil `db.New`'e kondu** — ilk çözüm `main`'de bir çağrıydı ve denetçi `_ = rlsRoleRefusal(...)` yazınca **kapı tamamen ölüyken iki test de yeşil kalıyordu**. ADR 0002: *çevreleme disipline değil yapıya dayanır*.
>
> 🔴 **İKİ KIRMIZI ÇİZGİNİN HİÇBİR MEKANİK KORUMASI YOKTU — ve ikisini de yalnız güvenlik merceği buldu.** `ALTER TABLE transactions NO FORCE ROW LEVEL SECURITY` sahibin bağlantısına yabancı bir tenant bağlamında **404.335 satır / 80.961 tenant** açıyordu ve **`internal/db` suiti tamamen yeşil kalıyordu**: izolasyon suiti `tappa_app` ile koşuyor, `tappa_app` sahip değil, `FORCE`'un tek işlevi ise RLS'i **sahibe** uygulamak. 17 tenant kapsamlı tablonun **11'inde** hiç `relforcerowsecurity` iddiası yoktu. İkincisi: `tappa_owner`'ın sahip olduğu bir **view** RLS'i **definer** üzerinden değerlendiriyor — aynı bağlantıda tablo **2** satır, view **392.105**. İkisine de gerçek kapı yazıldı, ve tablo listesi **canlı katalogdan** türetiliyor (bir migration'ın `ADD COLUMN tenant_id` ile sonradan kapsamlı yaptığı tablo da görünsün diye).
>
> 🔴 **§5: KİMSEYİ AYIRT EDEMEYEN SAKLI BİR ARALIK YER KANITI DEĞİLDİR — ve okuma tarafı ŞART.** `transactions` **değişmez** olduğu için yazma kapısı geçmişi düzeltemez; ölçüldü ki **dört satır zaten** böyle bir aralıktan `ip_match=TRUE` taşıyor ve asla düzeltilemez. Aynı yüklem artık iki tarafta da koşuyor (`internal/netx`). ⚠️ **Ve kural üç turda üç kez aşıldı, çünkü bir uzayı İSİM SAYARAK tanımlıyordu:** `0.0.0.0/1+128.0.0.0/1` kapatıldı → `10.0.0.0/8` tümleyeni (8 satır) kapatıldı → **`25.0.0.0/8` tümleyeni tek hane farkla geçti**, ve `192.0.2.0/24` tümleyeni (24 satır, dışarıda kalan blok **hiç yönlendirilmez**) **hiç kimseyi elemeden** geçti. Her seferinde sonuç aynıydı: `base:qr-requires-ip` **devre dışı**, `ok / trust=70 / "network proof of place"`. Çözüm ad listesini silip **büyüklüğe** geçmek oldu (aile başına en çok bir ISP tahsisinin iki katı) ve iki sayı **ölçülmüş meşru tavan ile sömürü tabanı arasına** çivilendi.
>
> ⚠️ **BİR KUSUR SINIFI YEDİ KEZ BLOKLAYAN BULGU ÜRETTİ: hiçbir kapının korumadığı bir sayı ya da ad sevk etmek.** `pool.go`'nun *"hiçbir test prod kurmuyor"* gerekçesi **kendi eklediği dosya** tarafından yalanlandı · `redline-check.sh` **aynı turda silinen** bir fonksiyonu canlı kapı diye gösterdi · kartın `F<n>` sayı tablosu **üç kez** yanlış çıktı (dördüncüsünde **silindi, düzeltilmedi**) · `Makefile`'ın test sayıları ölçüldükten sonra test eklendiği için bayatladı · ADR 0005'in *"0 satır `/0`"*ı adını verdiği DB'de **4 satır** verdi · `tenant.maxStaticRanges` **var olmayan bir sembol**tü · ve *"en geniş gerçek mekân listesi 264 adres"* cümlesinin **hiç göndergesi yoktu** (o 52 satır da test kalıntısıydı). **Kural: bir sayı ya bir kapıya bağlanır, ya tarihlenir, ya silinir.**
>
> ⚠️ **"KAPATMA, SAY" KURALI BU GÖREVDE İKİ KEZ KARARI VERDİ.** `redline-check.sh`'in R5b kuralı bir **grep**'tir ve SQL'i enumerate edemez: 1. tur 14 kaçış, 2. tur 11, 3. tur 9. Bitişiklik sınıfı **tek bir normalizasyon adımıyla** kapatıldı (desen desen değil), sonra kural **ne olduğuyla** yazıldı — bir **uyarı sistemi**, kapı değil — ve her sınıfın **bağlayıcı kapısı** haritalandı. Güvenlik denetçisi sekiz kaçışı ölçtü: **yedisinde bağlayıcı kapı kırmızıya döndü**, biri (`NO FORCE`) dönmedi — **bloklayan bulgu o birdi**, kaçışların kendisi değil.
>
> ✅ **YAPICI BİR TALİMATIMI ÖLÇÜMLE ÇÜRÜTTÜ VE HAKLIYDI:** eşiği *"azami bir `/8`"* diye verdim **ve** tamamen özel bir kurulumu (`10/8 + 192.168/16` = 16.842.752) kabul listesine koydum; ikisi **aritmetik olarak uyumsuz**. Kabul listesini otorite alıp sınırı 2^25'e koydu, `/8`'i **birim** olarak korudu. Gerekçe de doğruydu: fazla geniş bir listeyi kabul etmek **kimsenin görmediği değişmez bir satıra yanlış cümle yazar**, meşru bir listeyi reddetmek ise müdürün **ekranda okuduğu** bir hatadır.
>
> **4109 PASS / 0 FAIL / 0 SKIP · 24 paket** (`.env` yüklü; çıplak koşum **516 SKIP** — 469 üst düzey + **47 alt test**, eski komut alt testleri hiç saymıyordu) · `redline-check.sh` **exit 0** · ratchet **53/53** · `go.mod`/`go.sum` **değişmedi**. ⚠️ **`make audit` KIRMIZI ve bu turdan gelmiyor:** `govulncheck` 6 **stdlib** açığı, hepsi `go1.26.5 → 1.26.6` (**T31, kullanıcının işi**); `redline-check` yarısı exit 0. **Sıradaki:** M8-04 **FAZ B3**.

> **M8-03 DONE — 2026-08-19 (14. oturum), `624c555` + `ae07e7c`, 6 yapıcı turu / 3 RED.** Genel üçüncü göz **üç tur** (üç farklı ajan) + `tappa-security-auditor` **iki geçiş** (ikinci **ONAY**). Migration YOK, yeni bağımlılık YOK.
>
> 🔴 **KARAR MOTORU BUGÜNE KADAR TEK BİR VERDICT SATIRI BİLE LOGLAMIYORDU.** Kartın beş sinyalinin **beşinin de olayı yoktu** ve 5xx için **hiç erişim kaydı** yoktu. Yani kriter 3 *"eşik yaz"* işi değil, **sinyali VAR ETME** işiymiş. Altı olay sevk edildi ve adları **iki yönde** pinli: sabit yeniden adlandırılırsa derleme kırılıyor, **ve** runbook'un yapıştırılabilir sorgusunda geçtiği ayrıca doğrulanıyor — çünkü yeniden adlandırılan bir alan sorguyu **kırmaz, sessizce hiçbir şeyle eşleşmez**.
>
> 🔴 **LOG'U ZATEN BİR MAKİNE OKUYORMUŞ VE KART BUNU BİLMİYORDU.** Kümedeki bir SigNoz ajanı `/var/log/pods/*` topluyor, `exclude` listesi **`tappa`'yı saymıyor**, ve alıcısında **logfmt ayrıştırıcısı yok** — yani `key=value` gövdemiz **opak bir dize** olarak varıyordu. Prod artık `json`; geçersiz bir yazım **boot'u reddediyor** (sessiz geri düşme beş kuralı da öldürür ve *"sağlıklı"* görünürdü).
>
> 🔴 **VE KART KENDİ SALDIRI YÜZEYİNİ AÇMIŞTI — bunu YALNIZ güvenlik merceği buldu.** `request_id` aslında istemcinin gönderdiği `X-Request-Id`; aylardır bağlıydı ve **hiçbir log satırına ulaşmıyordu**, bu kart onu **her kayda** ulaştırdı. Ölçüldü: **900 KB'lık başlık → 921.757 baytlık tek kayıt**, 30 istek / 2 sn → node'un **50 MiB'lik tüm penceresi** siliniyor — ve o pencerede **yalnız log'da yaşayan** `tap.security_alert` ile `readiness.lost` var. Çalınmış oturumla tap eden biri **kendi uyarısını iki saniyede** yok edebilirdi. ⚠️ *"Gelen başlığı hiç kabul etme"* seçeneği **ölçümle elendi**: canlı ingress `$req_id`'yi **istemci adresiyle aynı satırda** yazıyor ve bizim kaydımız §4.7 gereği adres taşımıyor, yani o join operatörün **tek** atfetme kanalı. Sınırlandı (`[A-Za-z0-9._-]`, 64 karakter, uymayan **sessizce** değiştiriliyor — reddetmek log hijyenini çalışana karşı bir DoS koluna çevirirdi) → **183 bayt**.
>
> 🔴 **§4.7'DE İKİ BOŞLUK KAPANDI VE İKİSİ DE ÖLÇÜMLE BULUNDU:** tam GPS koordinatının **hiçbir mekanizması yoktu** — ne tip düzeyi ne mekanik (şimdi beş metot + yeni `R7c`); ve **R7, çok satırlı argümanları hiç görmüyordu** — 346 çağrı yerinin **131'i çok satırlı** ve **kartın kendi iki yeni olayı tam o kör noktadaydı** (`"cmac", req.CMAC` eklemek `make audit`'i **exit 0** bırakıyordu). Kapatma bedeli **3 yanlış pozitif**, üçü de **adıyla** muaf ve **her koşuda WARN olarak görünüyor**.
>
> ✅ **VE BİR TASARIM KARARI ORKESTRATÖRÜN HİPOTEZİNİ ÖLÇÜMLE DÜZELTTİ:** sonda yollarını erişim kaydından çıkarmayı önermiştim; yapıcı **yola göre değil, TASARLANMIŞ DURUMA göre** dışladı, çünkü toplu dışlama `/readyz`'in **500**'ünü ürünün **tek sessiz 5xx'i** yapardı. Aynı düzeltme günlük **5,1 MB**'lık sonda log'unu **0**'a indirdi ve tasarlanmış 503'ün *"sunucu bozuk"* alarmını **5 dakikada 60 kez** ateşlemesini durdurdu.
>
> ⚠️ **BİR KALIP ÜÇ TURDA ÜÇ KEZ TEKRARLADI VE ÇÖZÜMÜ LİSTE BÜYÜTMEK DEĞİLDİ:** `rotate-kek.sh`'ın temizleme listesi her turda **bir eksik** çıktı (`GOFLAGS` → `GOROOT` → `GOCACHEPROG`). Ölçüt **mekanikleştirildi**: bir test `go env`'in **kendi 46 adlık envanteri** üzerinde grep koşuyor ve script bir adı anmıyorsa düşüyor. Test **anmayı** sınıyor, temizlemeyi değil — bu sınır **yazılı**, ve `GOROOT`/`GOCACHEPROG` ayrıca davranışsal probe'larla tutuluyor. **T47 ve T49 kapandı.**
>
> **DURMA KURALI DERSİ — bu görevin en pahalı öğrettiği şey: kural MERCEK BAŞINA işler, görev başına değil.** Genel göz üç tur koşup yakınsamıştı (2. ve 3. tur **yalnız metin**) ve kural devreye girmek üzereydi. Sonra güvenlik merceği **ilk kez** koştu ve **üç §4.7 zayıflığı** buldu, biri kartın açtığı yeni saldırı yüzeyi. **Bir mercek yakınsadı diye görev yakınsamış sayılmaz.**
>
> **1779 PASS / 0 FAIL / 0 SKIP · 23 paket** (`.env` yüklü; çıplak koşum **449 SKIP** verip yine exit 0) · `internal/sun` **%97,0** · `redline-check.sh` **exit 0** · ratchet **53/53** · `go.mod`/`go.sum` **değişmedi**. ⚠️ **Teslimat kanalı YOK ve dürüstçe yazıldı → Q28 açıldı** (kart *"Q12'ye bağlı"* diyordu, Q12 **barındırma** sorusu). ⚠️ Saklama bir **süre değil BOYUT** (10Mi×5) ve gerçek üst sınır **"bir sonraki deploy"**; SigNoz TTL **doğrulanamadı**. **Sıradaki:** "ŞU AN" → **M8-04**.

> **M2-08 DONE + M8-05 FAZ A DONE — 2026-08-19 (14. oturum), 4 yapıcı turu / 2 RED.** Üçüncü göz **iki ayrı ajanla ONAY** + `tappa-security-auditor` **ONAY**. Migration YOK, yeni bağımlılık YOK.
>
> 🔴 **BİR RUNBOOK GÖREVİ, ÜRÜNÜN DOĞRULUK ÇEKİRDEĞİNDE SEVK EDİLMİŞ BİR KRİPTO KUSURU BULDURDU.** `internal/sun/verify_mac.go` → `sv2()` SDM sayacını SV2'ye **URL sırasında (verbatim)** yazıyordu. NXP **AN12196 rev. 1.8, s.10**, aynı UID için **aynı sayfada** iki şeyi birden yayımlıyor: §4.3 tablo 2 adım 4 `SDMReadCtr = 010000 (LSB first)` ve §4.4.1 URL'sinde `ctr=000001` — yani URL metni ile SV2 girdisi **kasten ters**. Sonuç: gerçek bir çipin sayacı **palindrom olmayan her tap'i reddedilirdi**; 3 baytta palindrom **1/256**, ve **1..255 aralığında hiç yok** → encode edilen ilk plaketin **ilk 255 tap'inin hepsi**. Belirti *"anahtar yanlış"* gibi görünürdü — ADR 0003'ün adını koyduğu tuzağın ta kendisi. **Üretimde tetiklenmedi**, ve gerekçe doğrulanabilir: encode aracı hiç yazılmadı ⇒ hiç plaket doğmadı (`cmd/` altında araç yok · `AuthenticateEV2First`/`ChangeFileSettings`/`ChangeKey` uygulayan tek satır Go yok · `db/queries/tags.sql`'de INSERT yok).
>
> 🔴 **VE KUSURU M2-04'ÜN "DÜZELTMESİ" ÜRETTİ — TEŞHİS DOĞRUYDU, DÜZELTMENİN YÖNÜ YANLIŞTI.** M2-04'ün 1. turu *"sayaç ters"* demişti ve haklıydı; sevk edilen çare *"ham baytları **verbatim** taşı"* oldu. 2. tur onu *"bağımsız Python CMAC ile kanıtlandı"* diye onayladı — **kanıt değildi**, çünkü Python **kodun kendi sırasını** yeniden uyguluyordu; iç-tutarlı bir zincirin ikinci kopyası bağımsız kanıt olamaz. Dış vektör **M2-07'ye ertelendi**, M2-07 **M8-05'e** erteledi. **Ve boşluk yalnız korunmasız kalmadı — ÇİVİLENDİ:** testin adı yanlış davranışı iddia ediyordu, yani doğru düzeltmeyi yapan kişi suite'i kırmızı bulup **kendi düzeltmesini geri almaya** yönlendiriliyordu. **Ders `agent-brief.md`'ye yazıldı ve o satırın yarısı SİLİNDİ** — *"baytlar verbatim geçmeli"* 22 gün boyunca **her yapıcının okuduğu kurallar dosyasında** duruyordu. Aynı belirsizlik `open-questions.md` Q05'te ve `.claude/skills/tappa-sun/SKILL.md`'de de vardı: **her ikisi de `SV2 = … || UID || ctr` diyip bayt sırasını hiç söylemiyordu.** Üçü de düzeltildi.
>
> ✅ **VE DIŞ KANIT İÇİN ÇİP GEREKMİYORMUŞ.** Kart *"gerçek çip kırmızı olabilir, bu adım atlanamaz"* diye uyarıyordu ve haklıydı — ama NXP kendi uygulama notunda anahtarı, UID'yi, SV2'yi ve oturum anahtarını **yayımlıyor**. `internal/sun/an12196_kat_test.go` (**yeni**) belgeden **transkribe edilmiş** 6 test taşıyor ve `referenceMAC`'e **hiç** dokunmuyor; 11 sabitin 10'u belgede birebir, 1'i **"DERIVED, NOT TRANSCRIBED"** diye etiketli. ⚠️ **Ve yapıcının ilk hâli kendi kanıtını ABARTTI** — *"Tablo 5 bayt sırasını Tablo 2'den bağımsız ikinci kez çiviliyor"* dedi; denetçi ölçtü: Tablo 5 **şifreli-PICCData** örneği, yayımlanan URL'sinde düz `ctr=` **hiç yok**. Metin **"ONE ANCHOR, NOT TWO"** olarak düzeltildi. **Değer-endian ekseni de kapandı** (`beUint24` zaten doğruydu).
>
> 🔴 **VE BİR DENETİM ÖLÇÜMÜ BAŞLI BAŞINA DERS:** ADR'nin NORMATİF bloğu *"ölçüm gününde `tags` tablosu boştu"* diyordu. Doğrulamanın bariz yolu **her zaman aynı yanıtı verir**: `DATABASE_URL` (`tappa_app` rolü) → **0**, `DATABASE_MIGRATE_URL` (owner) → **105 040**. RLS, `app.tenant_id` kurulmamışken **dolu bir tabloyu boş gösteriyor**. Cümle doğrulanabilir yokluk zinciriyle değiştirildi ve **tuzağın kendisi** ADR'ye + karta yazıldı.
>
> **M8-05 A/B'ye bölündü ve Q10 karara bağlandı: plaketleri KENDİMİZ encode ederiz** (tedarikçi encode ederse anahtarları tedarikçi bilir — Q06'nın reddettiği tek-nokta riski, bir kat yukarıda). **FAZ A done**: encode ayarları tablosu ADR 0003'ten normatif ve her atıf **revizyon-kapsamlı** (rev 2.0 bölümleri kaydırıyor: §3→§2 … §7→§6, ve **tablo numaraları türetilemez**) · anahtar teslimi/döndürme · anahtar hijyeni **6 mekanizma olarak doğrulandı** · plaket baskısı + QR'ın §5 bedeli · Q08 uyarısı · ve bölüm **"BU BİR PROSEDÜR DEĞİLDİR — BUGÜN ENCODE ARACI YOK"** diye açıyor. **FAZ B donanıma bloke**, 8 maddelik devir listesi runbook'ta. *(🔴 **İki cümle de 2026-08-20'de bayatladı:** liste **dokuz** maddedir — anahtar 0 maruziyeti eklendi —, ve FAZ B'nin **tamamı donanıma bloke değil**: B1 (ADR 0017) ve B2 (sunucu tarafı) donanımsız koşar, yalnız **B3** çipe bağlıdır. Ledger'a bak.)*
>
> ⚠️ **İKİ MEKANİZMA BOŞLUĞU KAPATILDI, İKİSİ DE DENETÇİ MUTASYONUYLA BULUNDU:** runbook tarayıcısı bölümün **başlığını** doğruluyordu ama **gövdesini** değil (başlık dururken 348 satır silinince test **yeşil** kalıyordu) → taban + çapa cümleleri eklendi · ve `subtle.ConstantTimeCompare` → `bytes.Equal` mutasyonu **tüm süiti geçiyordu** (R7 yalnız WARN + exit 0) → kaynak okuyan bir testle çivilendi. **Denylist ölçülerek elendi:** `string(a) == string(b)` yazılışı R7'nin desenine **sıfır** isabet veriyor. 🔴 **Kalan 14 çağrı yeri hâlâ pinsiz → backlog T53.**
>
> ⚠️ **VE SAYILMIŞ BİR LİMİT, KAPATILMADAN:** bu belgelerdeki **hiçbir atıf** mekanik olarak doğrulanmıyor — denetçi iki `.md`'ye **8 uydurma atıf** yerleştirdi (olmayan ADR, olmayan dosya, `base:qr-requires-gps`, `48 bayt`), **8'i de geçti**; ağaçtaki tek mekanik kontrol yalnız `Test*` adlarını çözüyor. `deploy/README.md` "Kabul edilmiş sınırlar" **madde 23** olarak yazıldı.
>
> **1725 PASS / 0 FAIL / 0 SKIP · 23 paket** (`.env` yüklü; çıplak `go test` **491 SKIP** verip yine exit 0) · `internal/sun` kapsam **%97,0** · `redline-check.sh` **exit 0** ve **R7 satırı yok** · ratchet **53/53** · `go.mod`/`go.sum` **değişmedi**. **Sıradaki:** "ŞU AN" → **M8-03**.

> **M8-02 FAZ C DONE — `0ce5615`, 11 tur, 5 RED.** Üçüncü göz **ONAY** + `tappa-security-auditor` **ONAY**. `make check` **exit 0** temiz ağaçta. Konu: **küme, `deploy/k8s/` manifestlerinin tarif ettiği hâle gelsin** — taze bir kurulum **elle müdahale olmadan** ayağa kalksın (T41 · T42 · T43 · kartın *"olay müdahalesi"* runbook kriteri). **0 adet `.go`**; değişen 7 dosya: `deploy.yml` · `deploy/README.md` · üç manifest · `verify-deployment.sh` · kart.
>
> 🔴 **ACİLİYET YAPISALDI, TERCİH DEĞİLDİ — VE ÖNCEKİ OTURUMUN ÖNGÖRÜSÜ AYNEN GERÇEKLEŞTİ.** Bir önceki commit (`960f5d5`) push edilince CI→deploy koştu (`31936836598`), `12-networkpolicy.yaml` kümeye **geri geldi** ve migration Job'ı **yine kesildi** (`BackoffLimitExceeded`, `failed=3`, `08:38:54Z→08:39:32Z`, üç pod `Error`). Yani *"kural şu an kümede yok"* diyen bir önceki blok, yazıldıktan **dokuz dakika sonra** yanlışlandı. Uygulama pod'u kesilmedi (koşan bir pod etkilenmiyor — ölçüldü) ve ürün ayakta kaldı.
>
> 🔴 **T41'İN TEŞHİSİ YANLIŞTI VE DÜZELTİLDİ (backlog).** *"Yarış değildi"* → **yarıştı**, ama Postgres'in değil **kuralın**: k3s yeni bir pod'un adresini izin kümesine **asenkron** yazıyor. Ölçüldü: izinli etiketli **5 taze pod → 5/5 ilk denemede ret**, 0,2–1,0 sn sonra kabul; kural silinmiş kontrol → **3/3 anında bağlantı**. `restartPolicy: Never` bir Job **sonsuza kadar** kaybediyor (her deneme yeni IP). Kural **kaldı ve uygulandığı iki kontrolle kanıtlandı**; çare **pod'da**: `wait-for-postgres` initContainer'ı. **T42'nin yazılı gerekçesi de yanlıştı** (bekleme **vardı**; gerçek pencere **DNS kaydının ~3 sn gecikmesi**) ve **T43 daralmadı, genişledi**: sebep sıralama değil **KEP-2535** — kubelet `NeverVerifyPreloadedImages`, yani kayıtlı kimliği olmayan pod düğümde imaj olsa bile **401** alıyor. **En keskin sonucu: `kubectl rollout undo` 401 alır**, yani olay anında ilk uzanılacak komut.
>
> 🔴 **BEŞ RED'İN DÖRDÜ TEK BİR SINIFTAN: "ölçülmeyen yol, gerçekte en olası olan yoldu."** (1) Kurtarma adımı `set -euo pipefail` altında `grep -v '^$'` yüzünden **her sağlıklı deploy'u** öldürüyordu — üstelik `secret/ghcr` yeni token'la ezildikten **sonra**, hiçbir apply'dan **önce**; 119 pod'a karşı test edilmişti, **sıfır pod'a karşı** değil. (2) at-risk kontrolü v1 at-risk kümede `ok` · (3) v2 Secret **hiç yokken** `ok` · (4) v3 **okunamayan** `managedFields` girdisini atlayıp eski damgayı kazandırıyor → `ok`. (5) Sonra hüküm kaldırıldı ama **README hâlâ `AT RISK` aratıyordu**. **Sağlıklı bir sistem BOŞTUR, arızalı bir sistem EKSİKTİR** — ikisi de kimsenin aklına gelen girdi değil.
>
> ✅ **VE SINIFI KIRAN ŞEY YAMA DEĞİL, İDDİANIN BİÇİMİ OLDU:** kontrol artık **hüküm basmıyor**, yalnız iki zaman damgasını basıp yorumu operatöre bırakıyor — ve doğruluğu savunulmuyor, **`grep` çözüyor** (`ok=0 · AT RISK=0 · safe=0 · 9 return, dokuzu da return 2 · 0 stderr yakalama`; orkestratör bağımsız doğruladı). Bu, ikinci durma kuralının (*"kapatma, say"*) ilk tam uygulaması.
>
> 🔴 **VE İKİ DENETÇİ MERCEĞİNİN AYRI OLMASI ÜÇÜNCÜ KEZ KANITLANDI:** 2. turda genel üçüncü göz **ONAY** verdi, aynı diff'te `tappa-security-auditor` **KRİTİK** buldu. Genel göz *"davranış değişiyor mu"* diye baktı ve seçicinin eşleşme mantığı **kusursuzdu**; güvenlik gözü *"bu adım ürünü ne hâle sokar"* diye baktı ve **boş girdi yolunu** ölçtü.
>
> ⚠️ **FAZ D'YE KALAN, KARTIN İKİ AÇIK KRİTERİ:** **yedek + PROVALI geri yükleme** (bugün yedek **yok**; M8-06 pilotu başlamadan kapatılmalı, çünkü pilot haftası §4.3 gereği yeniden kurulamaz) ve **KEK döndürme aracı** (*"tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan araç"* — repoda **yok**, yani bugün bir KEK sızıntısının yürütülebilir karşılığı yok). Ayrıca DPA/Q23 ve saklama süresi **hukuki** (B3), *"managed Postgres"* kullanıcı kararıyla **karşılanmıyor**.
>
> 🔴 **KULLANICIDA BEKLEYEN, VE ÜRÜN BUNA BAĞLI: PAKETLER HÂLÂ PRIVATE.** Ölçüldü: repo **PUBLIC** ama `ghcr.io/atknatk/tappa` ve `…/tappa-migrate` anonim çekmede **403** (anon token uzunluğu **0**) — yani repoyu public yapmanın **hedeflenen faydası oluşmadı**, bedeli (T1–T43 zayıflık haritası + 16 ADR kamuya açık) gerçekleşti. Çare ikisinden biri: paketleri **arayüzden** public yap (API yok, ölçüldü) ya da `read:packages` yetkili **uzun ömürlü bir PAT**'i `ghcr` sırrına yaz.

**Önceki güncelleme:** 2026-08-16 (11. oturum — 🟢 **ÜRÜN CANLI: https://tappa.everva.com.tr** · M8-02 yarım · repo **PUBLIC oldu (kullanıcı)** · T31 kapandı · sıradaki M8-02'nin kalanı)

> 🟢 **ÜRÜN CANLIYA ÇIKTI — 2026-08-16, `1194e23`.** `https://tappa.everva.com.tr` → `/healthz` **`ok`**, `/readyz` **`ready`**, `/` pazarlama sayfası. TLS Let's Encrypt, DNS **gri bulut**, migration **version 20**, tek pod `1/1 Running`. Küme: k3s tek node, Hetzner fsn1, `144.76.158.60`.
>
> **Q08 ve Q12 KULLANICI TARAFINDAN CEVAPLANDI** (domain + barındırma), yani M8-02 bloke değil. **CI üç kez yeşil**; ilk koşu ~140 testi düşürdü ve sebebi `state.md`'nin sekiz oturumdur yazdığı şeydi (`make migrate` hiç koşmuyordu) — kapandı. **T31 de kapandı**: `go1.26.6` ile govulncheck temiz, **kod değişikliği olmadan**.
>
> 🔴 **AMA ÜÇ ŞEY ELLE HALLEDİLDİ VE KÜME ŞU AN "OLMASI GEREKEN" HÂLİNDE DEĞİL — backlog T41/T42/T43.** En ağırı **T41**: `NetworkPolicy` migration'ı kesiyor (etiket eşleşmesine rağmen; k3s **RST** gönderdiği için belirti `connection refused` ve *"Postgres hazır değil"* diye okunuyor — ilk teşhizim de oydu, yanlıştı). **Kural şu an kümede YOK, elle silindi**, yani Postgres'e küme içi erişim **kısıtsız**; ve `deploy.yml:300` onu **her koşuda yeniden uyguluyor**, yani sonraki deploy migration'ı **yine kesecek**. ⚠️ **Bu, M8-02 denetçisinin "doğrulanamadı" dediği tam maddeydi** — statik olarak doğruydu, uygulanınca kesti. 🔴 **[2026-08-16, FAZ C'de GERİ ÇEKİLDİ — bu paragrafın üç cümlesi de artık yanlış.]** *"Kural kümede yok"* ve *"erişim kısıtsız"*: bu blok yazılıp push edilince CI→deploy koştu ve kural `08:38:50Z`'de **geri geldi** — yani cümle **dokuz dakika** yaşadı. *"Yine kesecek"*: **kesti** (`31936836598`, Job `BackoffLimitExceeded`). Ve `deploy.yml:300` **satır atfı zaten kayıktı**, `0ce5615` ile büsbütün kaydı — bu dosyada bir daha satır numarası yazma, **adım adı** yaz. Teşhisin kendisi de yanlıştı (etiket/hazır olmama değil, **asenkron izin kümesi yarışı**) → backlog **T41**, ve kusur `0ce5615` ile **kapandı**.
>
> 🔴 **VE REPO PUBLIC OLDU.** Kullanıcı paketleri public yapmaya çalışırken **repoyu** public yaptı (`gh repo view` → `PUBLIC`); paketler **hâlâ private** (anonim çekme 403), yani hedeflenen sonuç da olmadı. Baştaki kararı private'dı ve gerekçesi ölçülmüştü: repo `docs/backlog.md`'de **T1–T43**'ü, 16 ADR'yi ve her kabul edilmiş riskin gerekçesini taşıyor — **ürünün zayıflık haritası artık kamuya açık**. Geri alma tek komut (`gh repo edit --visibility private`), **kullanıcıya soruldu, cevap bekleniyor**. Bu arada çekme sorunu **başka türlü çözüldü**: deploy kendi `GITHUB_TOKEN`'ıyla `ghcr` sırrını yazıyor.
>
> 🔴 **VE ÜRÜNÜ İLK KEZ GERÇEKTEN ÇALIŞTIRMAK ÜÇ KUSUR BULDURDU — HİÇBİRİNİ HİÇBİR DENETİM BULAMAMIŞTI** (T38/T39/T40, hepsi 2026-08-15). Sebebi ortak: testler `httptest` ve `localhost` üzerinde koşuyor, yani **gerçek bir tarayıcının gerçek bir ağ üzerinden dayattığı kuralları hiç görmüyorlar**. **T38** — panelin origin kapısı, sayfanın kendi `<meta name="referrer" content="no-referrer">`'ı yüzünden `Origin: null` alıyor ve düz HTTP'de `Sec-Fetch-*` gelmediği için **localhost dışında hiç kimse giriş yapamıyor**. **T39** — aynı sebeple tarayıcı **konumu hiç vermiyor** (`isSecureContext=false`), yani §5'in dört kanıtından biri düşüyor. **İkisi de TLS ile kapandı** ve canlıda artık geçerli değil. **T40** — bir mekâna `0.0.0.0/0` verilebiliyor ve kayıt *"network proof of place"* diye yazıyor; **değişmez satırda yanlış bir kanıt beyanı**, hâlâ açık.
>
> **Bugün 12 commit.** M7-04 · M7-05 · M8-01 kapandı, ölçüm aracı onarıldı (`fadd9a7`), M8-02'nin paketleme yarısı çıktı, CI kuruldu. **`make check` exit 0**, **3507 PASS / 0 FAIL / 0 SKIP · 22 paket**.

**Önceki güncelleme:** 2026-08-15 (11. oturum — **M7-04 KAPANDI · M7-05 DONE · M7-07 AÇILDI (Q02'ye bloke) · 🎉 M7 KULLANICI DIŞINDA BİTTİ · M8-01 DONE · migration YOK · sıradaki M8-02**)

> **M8-01 DONE — 2026-08-15 (`4ddd11f`), 3 TUR / 0 RED.** Üçüncü göz **ONAY** + `tappa-security-auditor` **ONAY**. **M8 başladı: ürün artık paketlenebiliyor.**
>
> **🔴 KARTIN BEŞ KRİTERİNDEN ÜÇÜ ZATEN DOĞRUYDU — VE HİÇBİRİNİ HİÇBİR ŞEY TUTMUYORDU.** Commit **zaten gömülüydü** (Go'nun `-buildvcs`'i yapıyor, `-ldflags="-s -w"` onu **soymuyor**) ve **hiçbir şey okumuyordu**; disk bağımsızlığı bir `http.Dir` uzaktaydı; ve **`time/tzdata`, `internal/domain/tap`'ın tek satırı** yüzünden binary'deydi — ürünün gösterdiği **her tarihi** ve **§5'in geç kalma aritmetiğinin tamamını** taşıyan, **başka bir pakette duran** bir satır. **Görevin asıl işi kod yazmak değil, üçünü mekanik olarak denetlenebilir kılmaktı.**
>
> **🔴 VE DENETÇİNİN YİRMİ MUTASYONU, YAPICININ ON BEŞİNİN BULAMADIĞI YEDİ DELİK GÖSTERDİ — HEPSİ TEK SINIF: argüman taşıyan bir sabiti hiçbir şey pinlemiyor.** En ağırı: `defaultReadyTTL` **1 sn → 1 saat** yapılınca suite **yeşil** kalıyordu, yani `/readyz` **DB öldükten sonra bir saat boyunca `ready`** derdi ve orkestratör trafiği ölü sürece yollamaya devam ederdi. Diğerleri: probe zaman aşımı (2 sn → **20 dk** yeşil, ve testi **kendi 50 ms'ini** set ettiği için varsayılanı **hiç sürmüyordu**) · `Ping`'in tablo okumaması (`pool.go`'nun **uzun §4.5 argümanı** mekanizmasızdı; pin **`internal/db`'de yaşamak zorunda kaldı**, handler testleri o mutasyon altında `ok` kalıyordu) · açılış log satırının **DB dial'ından önce** olması · `nosniff`. Hepsi pinlendi, **hata mesajları değeri değil SONUCU** söylüyor.
>
> **ÜÇ KARAR, ÜÇÜ DE ÖLÇÜMLE:** derleme kimliği **hiçbir herkese açık uç noktaya** çıkmıyor (commit hash'i saldırgana **hangi kodun koştuğunu** söyler; operatörün zaten log'u ve `go version -m`'i var — ve `git tag` **boş**, yani dürüst sürüm araç zincirinin sözde-sürümü) · `/readyz`'in bütçesi **limiter değil ÖNBELLEK** (limiter belleği **saldırgan sayısıyla orantılı** harcar; ölçüldü: **10 sn flood, 50 worker, benzersiz query string, GET+HEAD karışık → tam 1 probe/sn**) · ve hazırlık **ping atıyor, tablo okumuyor**, çünkü `tappa_app` tenant bağlamsız **sağlıklı bir DB'de sıfır satır** görür (ölçüldü: owner **200.473**, app **0**) — tablo okuyan bir kontrol **başarısıyla hiçbir şey kanıtlamazdı**.
>
> **✅ VE BİR SINIR KAPANDI:** yapıcı *"işaret veritabanının binary'de olduğunu kanıtlar, `Europe/Malta`'yı **çözdüğünü** değil"* diye dürüstçe yazmıştı; kontrol ikilileri `/usr/share/zoneinfo` **olmayan** `alpine:3`'te koşuldu — tzdata'sız **`unknown time zone`, exit=3**; tzdata'lı **`04:00 local = 02:00 UTC`, exit=0**. **Süite girmedi** (repoda hiçbir test docker çağırmıyor, ve çapraz-derleme gerekiyor) ama **komutu ve çıktısı** testin yanına yazıldı.
>
> **✅ VE YAPICI BİR DENETÇİ BULGUSUNU ÖLÇEREK ÇÜRÜTTÜ:** güvenlik denetçisi *"`Allow: GET`, HEAD sayılmıyor"* demişti; gerçek artefaktta `Allow: GET` ve `Allow: HEAD` **iki ayrı başlık satırı** (chi `Add` kullanıyor, `Set` değil — `mux.go:527`, orkestratör bağımlılık kaynağından teyit etti). Kusur **kalkmadı, DARALDI**: sorun HEAD'in sayılmaması değil, `Header.Get` tarzı API'lerin **yalnız ilkini** görmesi ve `OPTIONS`'ın 405 alması → **M8-02**.
>
> **⚠️ M8-02'YE ÜÇ BORÇ, M8-03'E BİR:** `Allow`'u tek satırda birleştiren + `OPTIONS` yanıtlayan bir `MethodNotAllowed` handler'ı · **`vcs.modified=true` bir artefakt üretilebiliyor** ve tek sinyal bir WARN — `TAPPA_ENV=prod` altında onu reddeden **ne kod ne test** var · T28 (HSTS) ve T30 (`TAPPA_TRUSTED_PROXIES`) hâlâ açık (ikisi de **dağıtım kararı**; ⚠️ ölçüldü: `/readyz`'in **adres başına bütçesi olmadığı için** T30'u acil kılmıyor) · ve **kilit probe boyunca tutuluyor** (300 eşzamanlı, asılı DB → hepsi 503, **maks 2,001 sn**; tetikleyici **gerçek kesinti**, saldırgan üretemez) → **M8-03**.
>
> **3507 PASS / 0 FAIL / 0 SKIP · 22 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç, 252 s, load 2,05→3,58) · `redline-check.sh` exit 0 · `go.mod`/`go.sum` **değişmedi**. **Sıradaki:** "ŞU AN" → **M8-02**.

> **M7-05 DONE — 2026-08-15 (`145b344`), 6 TUR / 3 RED, MIGRATION YOK.** Üç **farklı** üçüncü göz + `tappa-security-auditor` (bu görevde **ilk** geçişi, **ONAY**). `/admin/account` sevk edildi; 00016'nın beş sütunluk grant'ının **üçü** yazılıyor, yeni yetki istenmedi.
>
> **🔴 KARTIN DÖRT KRİTERİNDEN BİRİ ZATEN SEVK EDİLMİŞTİ, ÜÇÜ ÖLÇÜLEREK DÜŞÜRÜLDÜ.** *"Fatura verileri, plan görünümü"* M6-12 B'de yapılmış (`billingactions.go:452-473`), yeni ekran **hiçbirini** basmıyor. Düşürülenler: **`vat_number` düzenlenemez** (global UNIQUE ⇒ bir düzenleme **tenant'ın dışına** ulaşır, başka bir işletmeyi kaydından edebilir; ayrıca `tappa_app` `vat_verified`'a UPDATE tutmuyor, yani satır VIES'in **hiç görmediği** bir numara için *"doğrulandı"* demeye devam ederdi) · **VAT yeniden kontrolü** (numara salt-okunurken sicile **aynı soru**; müşterinin tetiklediği dışa giden çağrı **farklı tehdit modeli**) · **`locations.timezone`** (Q01 istiyordu; ama `checkin.go:1007/:1023` **tap edilen** lokasyon için `emp.TenantTimezone` veriyor → o satırlar değişmeden eklenen sütun **yalan söyleyen bir sütun** olurdu). **Yokluklar pinli** — grant'ı veren mutasyon testi kırmızıya çeviriyor (iki ayrı denetçi doğruladı).
>
> **🔴 ÜÇ BLOKLAYANIN ÜÇÜ DE AYNI ŞEKİLDİ: ekran ya da defter, olmayan bir şeyi beyan ediyordu.** (1) Reddedilen kayıttan sonra *"On file"* doketi **kullanıcının yazdığını** basıyordu — `saves=0` iken sayfa `Europe/Maltaa`'yı işletmenin zaman dilimi ilan ediyordu ve gerçek değer sayfada **yoktu**; üstelik yorumu *"satırı yeniden okuyor ki sayfa onları atlamasın ya da İCAT ETMESİN"* diyordu. (2) **`Registered on` UTC'de basılıyordu** — Malta'da 00:30'da kaydolan işletme **bir gün öncesini** görüyordu, **üç satır altında kendi zaman dilimi yazarken**; panelin müşteriye görünen **on bir** tarihinin onu zaten `In(zone)` kullanıyordu. (3) **Kart kendi kendisiyle çelişiyordu** ve düzeltmesi **özyinelemeli** çıktı: madde 9, kodu **alıntılayarak** düzeltilmişti ve **aynı turun kendi değişikliği** o kodu değiştirmişti → alıntı, geri konulduğunda **suite'i kırmızı yapan** bir cümleye dönüştü. Çözüm alıntıyı güncellemek değil **silmek**; kartın tamamı tarandı, **altı** alıntı temizlendi.
>
> **🔴 İKİ KUSUR YALNIZ MUTASYON KOŞTURARAK ÇIKTI, OKUYARAK DEĞİL:** Tailwind sınıfı **Go'da** kuruluyordu — Tailwind `.templ`/`.js` tarar, `.go` **asla** → `bg-line/10` **sıfır kural** üretiyordu ve **dört VAT durumunun üçünde görünmezdi** (diğer tonlar başka şablonlar sayesinde vardı) · ve bir doket hücresine **hiçbir iddia ulaşmıyordu**, çünkü her kontrol **tüm belgeyi** tarıyordu ve **formun `value=`'su** onun yerine cevap veriyordu. ⚠️ **Aynı sınıf yapıcının kendi testinde de çıktı** (`strings.Contains(html, "2 February 2026")` iki blok aşağıdaki VAT damgasıyla eşleşiyordu → M23 **yeşil** döndü).
>
> **32 mutasyon, 30 yakalandı** — ve denetçinin cümlesi kayda geçti: ***"bu, koşuların kaydıdır, ağın kapsamının ifadesi değil"*** (bir denetçinin kendi sekiz mutasyonundan **ikisi**, yapıcının ulaşamadığı kör noktaları buldu).
>
> **⚠️ GÜVENLİK MERCEĞİNİN BLOKLAMAYAN AMA GERÇEK BULGUSU → backlog T37:** müşteri **zaman dilimini değiştirerek** ilk ücretli ayını **tam bir ay** öteleyebiliyor (ölçüldü: `UTC → 2026-04-01`, `Europe/Malta → 2026-05-01`). Üç hafifletme ölçülü: **donmuş ay kendi zone'unu taşıyor** · her değişim `timezone_before/after` olarak ize düşüyor · ekran bunu **açıkça söylüyor**.
>
> **3469 PASS / 0 FAIL / 0 SKIP · 20 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç, 246 s, load 1,87→2,75) · `redline-check.sh` exit 0. **Sıradaki:** "ŞU AN" → **M8-01**.

> **🔧 ÖLÇÜM ARACI ONARILDI — 2026-08-14 (`fadd9a7`), M7-05'ten AYRI COMMIT.** `make check` **her gün 22:00–24:00 UTC arasında kırmızı veriyordu ve ürün sağlamdı**. `advisories_db_test.go:77` günü `time.Now().UTC()` ile soruyordu; `ledger.Reader.page` ise pencereyi **tenant'ın zone'unda** çözüyor (`ledger.go:557-568`) — Maltalı bir tenant o saatlerde **zaten yarına geçmiş** oluyor, `now()` ile yazılan kayıt pencerenin dışına düşüyor ve test **kusursuz davranan** bir kısıtı suçluyordu. Düzeltme çözümü tekrar türetmedi, **sildi**: `Filter{}`'in sıfır `Date`'i zaten *"tenant'ın bugünü"* demek ve `Date.Zero()`'nun sözleşmesi bunu **zaten yazıyordu**.
>
> **Kardeş taraması: 10 yer, 2 düzeltildi, 8 ölçülerek güvenli.** Gizil olan `billing_db_test.go`: `CloseBillingPeriod` *ended*'i `tappa_local_month_start(month+1, tenants.timezone)` ile karar veriyor, fikstür ise ayı **UTC'de** istiyordu → ayın son saatlerinde `Close`, `ErrPeriodNotEnded` yerine **`nil`** dönerdi. Sonda: **4 ayın son günü × 24 saat = 96 saat, 6'sında ayrışıyor**.
>
> ⚠️ **VE KANIT ZAMANDAN BAĞIMSIZ KURULDU** — saat 22:30'da yapılan bir düzeltme başka türlü hiçbir şey kanıtlamaz. Saati oynatmak yerine **karşılaştırmanın diğer tarafı** oynatıldı: `Etc/GMT-14` ile `Etc/GMT+12` **26 saat** aralıklı, yani **her an** en az biri UTC'nin takvim gününden farklı (ve yardımcı seçimini **doğruluyor** — `Etc/*` işaret konvansiyonu **ters**). 96 sentetik saatte (sıradan gün · ay sınırı · yıl sınırı · AB ileri saat günü) geri alınan düzeltme **her saatte kırmızı**. **Ders, pozitif kontrolden çıktı: Europe/Malta 96 saatin 89'unda UTC ile UYUŞUYOR** — orijinal kusur tam olarak böyle saklanmıştı.

> **M7-04 FAZ B DONE → M7-04 KAPANDI — 2026-08-14 (`01a07bc`), 6 TUR / 2 RED, MIGRATION YOK.** Üç **farklı** üçüncü göz + `tappa-security-auditor` **iki geçiş**; son turda ikisi de **ONAY**. Dört kimliksiz rota; alıcı adresi mevcut `GetAdminByID`'den geliyor, yani *"link yalnız satırdaki adrese"* bir sözleşme değil **yapı**. **40 mutasyon, 37 yakalandı.**
>
> **🔴 SON ÜÇ TURUN BLOKLAYAN BULGUSUNUN İKİSİ, BİR ÖNCEKİ TURUN DÜZELTMESİNİN YAN ETKİSİYDİ.** (1) Güvenlik merceği **sömürdü**: hesap denetim bütçesi `admin.recovery.completed`'ı da kapsıyordu → **11 kimliksiz istek**, kurbanın gerçek parola değişikliğinin `audit_log`'a hiç düşmemesini sağlıyordu (`completed=0` ölçüldü) — ve **emsal aynı paketteydi ve tersiydi** (`ActionAdminLoginSucceeded` bütçeden muaf, yani *"başarılı durum geçişi susturulamaz"* kuralı repoda **zaten vardı**). Tek bütçe **üçe** bölündü, ölçüt: *"kimliksiz bir çağıran bu satırın hacmini üretebilir mi"*. (2) O düzeltmenin `Submit`'e eklediği fail-closed 404, kriter 2'nin **süreç-log kolunu** da öldürdü — ama defter hâlâ o kolun canlı olduğunu yazıyordu (**dört istek, 0 satır** ölçüldü). **Cümle küçültülmedi, DOĞRU YAPILDI**: 404'ten önce sınırlı bir log satırı. **Ders: bir düzeltmeyi denetlerken ilk soru "bu ne kırdı" olmalı — sayılmış-açık defteri, kendisini yanlışlayan düzeltmeyi görmüyor.**
>
> **🔴 ORKESTRATÖRÜN TERCİHİ ÜÇÜNCÜ KEZ ÖLÇÜMLE ELENDİ.** Kanalın hata metnindeki sızıntı için **sınırda temizlemeyi** önerdim (handler her iki sırrı da tutuyor); yapıcı onu **inşa etti** ve altı **sıradan** SMTP hata şeklinden geçirdi — **ikisi sızdırdı** (base64'lenmiş gövde · büyük harfe çevrilmiş adres). Denylist yerine **yapısal imkânsızlık**: metin loglanmıyor, satır **hata TÜRÜNÜ** taşıyor. Artık (teşhis edilebilirlik) **yazıldı ve sahibi adlandırıldı**.
>
> **🔴 VE BİR CÜMLE HİÇBİR ŞEY TUTMUYORDU.** *"Üç tüketici süreç başına karşılıklı dışlayıcı"* — bölme **{1,2} vs {3}**'tü; iki tüketici teslimat açık bir kurulumda **aynı anda canlı** ve aynı kovayı harcıyor. Kurtaran şey yazılı olmayan **20 + 10 ≤ 60** aritmetiğiydi ve **onu pinleyen hiçbir test yoktu** (denetçi mutasyonla gösterdi: üçünü eşzamanlı erişilebilir kılınca paket **yeşil** kaldı). Sabitlerin toplamı tavanı geçtiği gün **sessizce** bir açlık kanalı doğardı — ve o kanal §4.6'yı **oran sınırıyla** susturur, yani 1. turun RED'inin tam sınıfı. Artık pinli.
>
> ⚠️ **BEŞ KRİTERİN ÜÇÜ + İKİ SAVUNMA SEVK EDİLEN YAPILANDIRMADA ULAŞILAMAZ** (`TAPPA_RESET_DELIVERY=none` — Q02 cevapsız olduğu için **tek yasal değer**): kriter 1'in (a) yarısı · kriter 2'nin `audit_log` kolu · kriter 3'ün **tamamı** · `adminResetSubmitLimit`'in kapısı · `cookieSafe`. Kanıtları **gözlem değil sahteler**. **Hiçbir teslimat kanalı sevk edilmedi ve bu bir karardır:** dürüst bir geçici kanal yok — linki adresi yazan kişiye göstermek, ADR 0015'in adını koyduğu **ele geçirmenin ta kendisi**.
>
> **🔴 VE KARTIN BAŞLIĞI ÖLÇÜLDÜ: "Admin daveti" HİÇ YAPILMAMIŞ.** Beş kabul kriterinin **hiçbiri** onu istemiyordu, iki fazın hiçbirinde yapılmadı, ve iki denetçi de görmedi **çünkü brief'lerinde yoktu — orkestratörün kaçırması**. `CreateAdminUser`'ın tek üretim çağrı yeri `signup.go:719` ve `role: "owner"` yazıyor → bir işletmenin **tam olarak bir** yöneticisi olabiliyor, `manager` **üründe ulaşılamaz**. Kart daraltıldı, **M7-07 açıldı**, Q02'ye bloke (bugün yapılırsa **ikinci bir ölü özellik**).
>
> **3389 PASS / 0 FAIL / 0 SKIP · 20 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç) · `redline-check.sh` exit 0 (3 önceden var olan WARN). **Sıradaki:** "ŞU AN" → **M7-05**.

> **M7-06 DONE — 2026-08-14 (`c8e763a`), MIGRATION 00020, ADR 0016.** 🔴 **KULLANICI TALİMATIYLA DOĞDU:** *"benden beklenenleri admin panelden girilebilir yap… bunu bekleyip durma benden"* + *"süper admin girsin güncelleyebilsin, bu kadar basit"*. M7-01'in dört yasal metni **üç oturumdur** kullanıcı bekliyordu; artık `.env`'e admin id yazılıyor, normal hesapla girilip `/admin/legal`'den yayımlanıyor.
>
> **SÜPER ADMIN YOKTU VE YAPILMADI.** `admin_users.role` kapalı sözlüğü `('owner','manager')` ve onu **policy motoru okuyor** (`actor:role`) — yeni bir rol, motorun o rol karşısındaki kararını da getirir. Ayrı bir operatör girişi ise **yeni bir kimlik doğrulama yüzeyi**. Dört belge için ikisi de pahalı → **var olan giriş + `.env` izin listesi + tek ekran**. **Gerçek operatör paneli M9-08'e** yazıldı, borçlarıyla.
>
> **🔴 ORKESTRATÖRÜN ÖNERİSİNİN YARISI SÖMÜRÜLDÜ.** İzin listesini **e-posta** ile anahtarlamayı önerdim; güvenlik denetçisi uçtan uca kırdı: `admin_users` e-posta tekilliği **tenant başına** (`UNIQUE (tenant_id, email)`), `/signup` **herkese açık**, **e-posta doğrulaması yok** → listedeki adresi bilen herkes o adresle **kendi işletmesini** kaydedip girdi (**GET 200 · POST 303 · yayımlanmış politika değişti**). Anahtar **`admin_users.id`**'ye taşındı — PK, global tekil, **veritabanı atıyor**, hiçbir form beyan edemiyor. **Ders: bir izin listesi, ANAHTARININ TEKİLLİĞİ kadar değerlidir.**
>
> **§6'YA GEREKÇELİ İSTİSNA:** `legal_documents` **`tenant_id` taşımıyor** (ADR 0016). Eleyen ölçüm: operatörün admin hesabı bir **müşteri tenant'ında** yaşıyor, yani içerik *"Tappa'nın tenant'ında"* olsaydı yazma yolu **müşteri kimliğiyle ulaşılabilen bir handler'da** `WithTenant(başkasınınTenantı)` çağırmak zorunda kalırdı. ⚠️ **Fiyatı gizlenmedi, ölçülüp yazıldı:** RLS olmadığı için yazma tarafında **kuşak+kemer yok** (yabancı tenant bağlamında INSERT **başarılı**, ölçüldü) — tek kontrol **uygulama izin listesi**; ayakta kalan koruma **append-only** + `slug` CHECK'i. `redline-check.sh`'in muafiyet sözdizimi **repoda ilk kez** kullanıldı, **her koşuda WARN**.
>
> **3345 PASS / 0 FAIL / 0 SKIP · 20 paket** · `make check` **exit 0** (`9124dcb` sonrası) · 11 mutasyon, 11'i yakalandı (**biri ancak taşındıktan sonra**: render yolu paragrafları yeniden bölmekte serbestti).
>
> 🔴 **VE `make check` BU DOĞRULAMADA ÜÇÜNCÜ BİR FLAKE YAKALADI — ÖLÇÜLÜP KAPATILDI (`9124dcb`).** `TestTags00013_ConstraintsNoOtherTestNames` bir `randUID`'i küçük harfe çevirip 00013'ün kanonik-hex CHECK'inin reddetmesini bekliyordu; `randUID` **14 büyük-harf hex hanesi** üretiyor ve **tamamı rakam olduğunda** `ToLower` bir **no-op** oluyor → ifade haklı olarak kabul ediliyor ve test, kusursuz davranan bir kısıtı **adıyla suçlayarak** kırmızı veriyor. **Ölçüldü: 200.000 çekilişte 298 = 1/671** (öngörü `(10/16)^14` = 1/721); düzeltmeyle **0/200.000**. **Ders: bir YAZILIŞ iddiası, yazılışı farklı olabilen bir değer ister — "rastgele" onu garanti etmez.**

> **M7-04 FAZ A DONE — 2026-08-14 (`b039ef3`), MIGRATION 00019, ADR 0015, 3 TUR / 1 RED.** Üçüncü göz + `tappa-security-auditor`. **Kartı ölçmek ON ÜÇ MIGRATION BOYUNCA AÇIK KALMIŞ CANLI BİR YETKİ YÜKSELTMESİ buldurdu.**
>
> 🔴 **`password_resets` 00006'dan beri duruyordu** — RLS beşlisiyle, ama **tablo geneli GRANT'la** ve `db/queries`'de **hiç geçmeden**. Ölçüm (`tappa_app`, kendi tenant bağlamında): canlı bir sıfırlama token'ının `admin_user_id`'sini **aynı tenant'ın owner'ına** çevirmek → **`UPDATE 1`**. Yani kendisi için sıfırlama isteyen bir manager, elindeki linkle **owner'ın parolasını** yazabilirdi. **RLS göremez — aynı tenant.** Kimse fark etmemişti çünkü *"hiçbir kod yolu kullanmıyordu"* — yapıcının cümlesiyle **bu bir kontrol değil, geri sayım**, ve M7-04 tam o yolu açacak görevdi. Yedi sonda, hepsi **önce çalışıyor / sonra reddediliyor**; **A3 (`used_at=NULL`) KAPANMADI** ve dürüstçe yazıldı (`employee_invites` de aynı durumda → **devralınmış**, yeni sınıf değil).
>
> **KAPSAM DARALTILDI: magic link YOK.** Eleyen ölçüm: ayrık koşulun sağ kolu (**şifreyle giriş**) **M6-01'de sevk edilmiş**, yani kriter zaten karşılanıyor; sol kol **net yeni saldırı yüzeyi**. Fark veri katmanında: sıfırlama satırı **tek durum geçişi** verir ve **oturum vermez** — çalınan sıfırlama linki kurbana **görünür** bir şey yaptırır, çalınan magic link **sessiz bir giriştir**.
>
> **🔴 ON ÜÇ MUTASYONUN ÜÇÜ YEŞİL DÖNDÜ VE ÜÇ İDDİA ÇÜRÜTTÜ:** bir test guard'ı değil **`WithTenant`'ın rollback'ini** ölçüyormuş · *"sıra transaction içinde belirleyicidir"* **fazla iddiaymış** (bugün işi yapan **transaction**; sıra, birincisi kırıldığı gün işe yarayan **ikinci kemer** — ölçüldü: bölünmüş transaction + ters sıra → tekrar oynatılan link **2 canlı oturumun 2'sini** iptal ediyor) · yeni kemer tarayıcısının kuralı **çok-ifadeli CTE'de boş**muş (*"en az bir WHERE"* yetiyordu).
>
> **🔴 VE PROJENİN ÜÇÜNCÜ HESAP KİLİDİ SINIFI BULUNDU — `retired` CTE'sinde.** Saldırgan herkese açık forma **kurbanın adresini** yazar; her istek kurbanın **canlı** linkini emekliye ayırır (`retired_count=1`), kurbanın `Consume`'u **0 satır** döner. Tekrarlanırsa hesap sahibi kurtarmayı **hiç tamamlayamaz**. ⚠️ **İmza kusuru:** ADR 0015 tam bu zarar sınıfını **reddedilen alternatif 4**'te adıyla tanıyor, sonra **sevk edilen tasarımda zayıf sürümünü üretiyor**. Artık kabul edilen risk, ve çaresi (**adres başına oran sınırı**) kartın **kabul kriterlerine** yazıldı.
>
> **3307 PASS / 0 FAIL / 0 SKIP · 19 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç) · `redline-check.sh` exit 0 · `Down` taze klonda **IDENTICAL** (790 satır). **Sıradaki:** "ŞU AN" → **M7-04 FAZ B** (Q02 kullanıcıda).

> **M7-03 FAZ B DONE — 2026-08-14 (`8a985eb`), 5 TUR / 2 RED.** Üç **farklı** üçüncü göz + `tappa-security-auditor` **iki geçiş**. **Kırk saniyelik bir işletme artık tarih seçiciye yollanmıyor:** `/admin` üç `tags` sayımından türeyen bir bildirimle açılıyor, ve tarih tavsiyesi **plaket durumuna değil** *"bu işletmenin hiç kaydı var mı"*ya bağlı — çünkü **manuel kayıt plaket istemez**.
>
> **🔴 KARTIN BİR CÜMLESİ ÜRÜNÜN KENDİ VERİSİNDE TERS ÇIKTI.** Kart *"`structure='multi'` departmanları açar"* diyordu. Ölçüm: **KF = `multi`, 9 lokasyon, 0 departman**; **KM = `single`, 5 departman, 5/5 kendi vardiyasıyla**; çalışan bağlılığı **0/3664** vs **10/11**. Ve **`tenants.structure` `CreateTenant`'tan sonra hiçbir ifade tarafından okunmuyor**. Yani eksik olan `multi` değil, **`single`'a hiçbir şey söylenmemesiydi** — sihirbaza dördüncü adım eklenmedi, cümle **iki şekle birden** ait hâle getirildi. **Elenen şık: 3.**
>
> **🔴 İKİ RED'İN İKİSİ DE "DÜZELTTİM" DENEN ŞEYİN DÜZELMEDİĞİNİ GÖSTERDİ.** (1) Teslimat tarayıcısı **yerine geçtiği literali** (*"posted to you"*) kaçırıyordu — desenler **nesne zamiri** dayatıyordu — ve **pozitif kontrol delik açıkken yeşildi**, çünkü örnek cümleyi **desenlerden üretip** *"her desen eşleşti mi"* diye soruyordu. (2) Advisory **kademelendirmesi üretimde etkisizdi**: advisory hatası transaction'ı **abort ediyor** → `Commit` → `ErrTxCommitRollback` → müdür **yine 500**; ve testi bunu göremiyordu çünkü **`Screen`'i hiç çağırmıyordu**. ⚠️ **Bu ikincisi, yapıcının BİR TUR ÖNCE kendi bildirdiği kusurun tekrarıydı** (*"bayrakların tasarlandığı bozulma yolu ulaşılamaz"*). Çözüm **savepoint** — ve yan bulgu: **abort olmuş transaction'da `SAVEPOINT` bile reddediliyor (25P02)**, yani *"sonradan tamir et"* çözümü makul görünüp **hiç çalışmazdı**.
>
> **VE BİR AĞ SEKİZ GÜNDÜR YARIM KOŞUYORDU:** `make audit`'te govulncheck ilk satır, T31 yüzünden exit 2, make orada duruyor → **`redline-check.sh` hiç koşmuyordu**. Düzeltildi; ayrıca `rg` **yok/bozuk/çalıştırılamaz** ya da **taraması patlıyorsa** artık **exit 2** (*"atlandı ≠ temiz"*), **1**'den ayrı (o *"ihlal bulundu"*).
>
> **3223 PASS / 0 FAIL / 0 SKIP · 19 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç) · `redline-check.sh` yedi yolun yedisi ölçüldü. **Sıradaki:** "ŞU AN" → **M7-04**.

> **M7-03 FAZ A DONE — 2026-08-14 (`81ce4d5`), MIGRATION 00018, ADR 0014, 5 TUR / 2 RED.** İki **farklı** üçüncü göz + `tappa-security-auditor`. **`admin_users.password_hash` artık ŞEMA DÜZEYİNDE işlenebilir bir bcrypt digest'i olmak zorunda** — M7-02'nin devrettiği **(b) limiti kapandı**.
>
> **NEDEN ŞİMDİ:** `password.go` gerekçenin **ne zaman biteceğini adıyla yazmıştı** — *"a task that opens a write path to admin_users … **M7-02 (public sign-up)**"*. M7-02 o yolu açtı (`CreateAdminUser` + 00017'nin `password_hash`'i **hem INSERT hem UPDATE** grant'ına koyması), yani *"bugün delik değil"* argümanı **süresi dolmuş** bir argümandı. Kısıt: `^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$`, `NOT VALID`.
>
> **🔴 ORKESTRATÖRÜN SUNDUĞU İKİ ŞIKKIN İKİSİ DE ELENDİ, ÖLÇÜMLE.** *"Yalnız biçim"* **tutarlı bir seçenek değil**: `[0-9]{2}` taslağı `$2a$99$…`'yi kabul eder ve **100 maliyetin 72'si** bcrypt'te 0–2 µs'de hata verir — **aynı 1,9M× kol, başka kapıdan**. Ve *"cost ≥ 10"* şıkkı **+156 s/paket** ölçüldü, elendi. **Ders: iki şık sunmak, ikisinin de doğru olduğu anlamına gelmiyor — yapıcı üçüncüsünü ölçtü.**
>
> **🔴 TAVAN 14, VE SEBEBİ M7-02'NİN DOLGU TASARIMI.** `manager.go:475` dolguyu **`candidates[0]`'ın digest'inden** fiyatlıyor ve **her** adayı karşılaştırıyor → tek yavaş satır **başkalarının** girişlerini kilitliyor. cost 31 ≈ **31 saat** tek karşılaştırma, 8 adayda **~249 saat**. Tavan koymamak *"bugün 0 kazandırıyor, **132.000× yarıçap** kaybettiriyor"*.
>
> **🔴 VE BİR TEST VACUOUS ÇIKTI — MUTASYON YAKALADI.** İlk sürüm Go tarafı otoritesi olarak `bcrypt.Cost`'u kullanıyordu; **`Cost` salt'ı ÇÖZMÜYOR** (`x/crypto` salt'ı kopyalıyor, `base64Decode` `expensiveBlowfishSetup` içinde). `$2a$04$` + 22×`!` + 31×`c` → `Cost` mutlu, `Compare` **21 µs**'de düşüyor. Kısıtın gövde sınıfı gevşetilince **eski test yeşil kaldı**. Oracle artık `CompareHashAndPassword`'ın **hata TÜRÜNE** bakıyor. **Ders: bir testin otoritesi, koruduğu şeyi gerçekten ölçmeli — adı benzeyen bir fonksiyon yetmez.**
>
> **VE BİR YAN BULGU GÖREVİN DIŞINA TAŞTI:** `password.go`'nun zamanlama tablosu `4bc2e72`'den beri `darwin/arm64` diyordu. **Bu makine Intel MacBookPro16,1** — Apple Silicon değil, Rosetta değil (`uname -m` → `x86_64`, `GOARCH=amd64`). Aynı yanlış inanç **backlog B2'yi** (*"kullanıcı arm64 Go kursun"*) **beş oturumdur** taşıyordu; **B2 düşürüldü**. ⚠️ `376-427 ms` bandı **bilerek aşağı çekilmedi** — `manager_timing_test.go`'nun pinli **380 ms** bütçesinin kaynağı o, ve güvenlik payı sessizce indirilmez.
>
> **3184 PASS / 0 FAIL / 0 SKIP · 19 paket** · `make check` **exit 0** (commit sonrası, temiz ağaç) · ⚠️ `make audit` **exit 3** ve sebebi **govulncheck** (6 stdlib açığı, `go1.26.5 → 1.26.6`) — **backlog T31, kullanıcının işi**; `redline-check.sh` tek başına exit 0. **Sıradaki:** "ŞU AN" → **M7-03 FAZ B**.

> **M7-02 DONE — 2026-08-13 (`9ac3065`), MIGRATION 00017, ADR 0013, 8 TUR / 4 RED — PROJENİN EN UZUN GÖREVİ.** Üç **farklı** üçüncü göz + `tappa-security-auditor` **iki kez**. **Artık herkes kayıt olabiliyor:** `/` → sihirbaz → tenant + mekânlar + ilk sahip **tek transaction'da** → giriş → panel, uçtan uca canlı sürüldü.
>
> **🔴 SEKİZ AY ÖNCEKİ BİR MIGRATION BU GÖREVİ ADIYLA ÇAĞIRIYORDU — VE ALTINDAN BELGELENENDEN DAHA KÖTÜSÜ ÇIKTI.** `00011`'in *"süresi doluyor"* uyarısı doğruydu, ama gerçek kusur DoS değildi: `MaxCandidates=8` kapağı **ödeyen müşteriyi kendi panelinden kilitliyordu**, beş koşunun üçünde. **Ders: bir kabul-edilen-risk notu 'süresi doluyor' diyorsa, süresi dolduğunda ölçülecek şey NOTUN TARİF ETTİĞİ ZARAR DEĞİL, KORUMANIN KENDİSİDİR.**
>
> **🔴 VE BİR GÜVENLİK KARARININ DAYANAĞI ÇÜRÜDÜ — ORKESTRATÖRÜNKİ.** Zamanlama kanalını kapatma kararını *"bütçe zaten sekiz karşılaştırma için boyutlanmıştı"* cümlesine dayandırdım; o cümle **hata yolu için doğru, başarı yolu için yanlıştı** ve hiçbir şey onu ölçmüyordu. Onarınca çıkan tavan **M7-02'den eski** bir deliğin de altına indi. **Ders: bir takası satın alan cümleyi, takası uygulamadan ÖNCE ölç.**
>
> **🔴 MUTASYON DİSİPLİNİ ÜÇ KEZ YAPICININ KENDİ TESTİNİ YAKALADI**, üçü de hata varken **yeşil** dönüyordu. Sonuncusunun dersi kalıcı: ***"bir reddediş, neyin reddettiğini bilmediğin sürece hiçbir şey kanıtlamaz"*** — INSERT yabancı anahtar tarafından reddediliyordu, yetki tarafından değil.
>
> **⚠️ ÖLÇÜLMÜŞ SINIR — `internal/handler` ~256 s** (dolgu öncesi 154,7–197,1 s bu makinede). Kalıntı `TestPanelE2E_TimingIsFlatOverHTTP` ~93 s: dört hücresinin ikisi hiçbir şey çözmüyor ve fallback `adminauth`'ta **unexported**. **Kabul edildi (orkestratör kararı):** dolgu **hafifletmenin kendisi**, bedelinin bir zamanlama testinde görünmesi kusur değil. Reddedilen iki lever: **test dikişi export etmek** (güvenlik özelliğini yapılandırma hatasına çevirir) ve **hücre düşürmek** (hücreler özelliğin kendisi).
>
> **3152 PASS / 0 FAIL / 0 SKIP · 19 paket** · `make lint`/`redline-check.sh`/`make audit` **ayrı ayrı** exit 0 · `make fmt gen` manifest `050e970a…` **önce=sonra** · orkestratör geriye tarihli INSERT sondasını **kendi tekrarladı** → `permission denied`. **Sıradaki:** "ŞU AN" → **M7-03**.

> **M7-01 DONE — 2026-08-13 (`3516f59`), 5 TUR / 1 RED, ADR 0012.** İki **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**. **`/` artık 404 değil — ürünün kimliksiz ulaşılabilen İLK yüzeyi.**
>
> **🔴 RED, TAM DA BRIEF'İN ETRAFINA KURULDUĞU KUSURDU.** Sayfa **mount edilmemiş bir yetenek** ilan ediyordu (*"headcount split by department"* — `department` billing'in beş dosyasında da **0** kez), ve o yasağı **yapıcının kendisi yazmıştı**. Asıl sebep dikkatsizlik değil **ağ yokluğuydu**: fiyat, bedava ay, çerez, slogan ve tablo pinliyken o blok **hiçbir testin kapsamında değildi**.
>
> **🔴 VE DÜZELTMENİN SINIRI ÜÇ YERE YAZILDI.** `Claim` artık çapasını adlandırmadan derlenemiyor, her çapa **ürünü okuyor** — ama denetçi geçen bir çapa + **yalan** bir cümle enjekte etti ve paket **yeşil** kaldı. Mekanizma *"kaybolan yeteneği yakalar, hiç var olmamışı yakalayamaz"*, ve bu **testin içine** yazıldı. **Ders: bir ağ, yakalayamadığını söylemediği sürece olduğundan güçlü görünür.**
>
> **🔴 `redline-check.sh` R1'E MUAFİYET — §4'ü zorlayan mekanizmanın kendisi.** Güvenlik merceği **ilk hâlini kırdı**; üç koşulla daraltıldı (**yola sınırlı · ifade-kapsamlı · her koşuda `[R1 · WARN]`**) ve **ADR 0012** yazıldı. ⚠️ **Muafiyetin yarısı gereksizdi** — denetçi reponun kendi örneğini buldu (`activate.templ:76`), metin o kalıba getirildi ve ifade **kaldırıldı**. **Ders: bir muafiyet istemeden önce, metnin kendisinin yeniden yazılıp yazılamayacağını ölç.**
>
> **Ters tuzak:** `documentHead` **her sayfaya** `noindex` gömüyordu — pazarlama sayfası kimsenin bulamayacağı sayfa olurdu.
>
> **Yasal metinler bilinçli olarak YAZILMADI** (uydurmak hukuki olarak yanlış belge üretirdi). **Kullanıcıdan bekleniyor:** şirket künyesi (tüzel kişi adı · Malta sicil no · kayıtlı adres · VAT) · veri sorumlusu + iletişim · saklama süreleri (mesai kaydı · oturum · `audit_log`) · sözleşme/fatura şartları. **Çerez bildirimi GERÇEK** — altı satır çerezi yazan `http.Cookie` literalinden okunuyor.
>
> **2960 PASS / 0 FAIL / 0 SKIP · 18 paket** · `make check` **exit 0** (commit sonrası, manifest `9584470b…` önce=sonra) · `make audit` · `make lint` · `redline-check.sh` **ayrı ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M7-02**.

> **🎉 M6 KAPANDI — MÜDÜRÜN GÖRDÜĞÜ TARAFIN TAMAMI BİTTİ.** Sekiz panel bölümü: transactions · review · employees · locations · reports · anomalies · policies · **billing**. **Geriye M7 (portal & signup) kalıyor — ve bugün HİÇ KİMSE KAYIT OLAMIYOR, ürünün en büyük fonksiyonel boşluğu orası.**
>
> **M6-12 FAZ B DONE — 2026-08-13 (`5b2736a`), 3 TUR / 0 RED.** Üçüncü göz **1. turda ONAY** (projede nadir) + `tappa-security-auditor` **ONAY**. `/admin/billing` + CSV + founding uyarısı + iki POST.
>
> **🔴 YAPICI ORKESTRATÖRÜN ÇERÇEVESİNİ ÖLÇEREK ÇÜRÜTTÜ — VE HAKLIYDI.** Brief *"rol kapısını policy motoruna bağla, M6-09 B'nin yolu"* diyordu. Üç ölçüm: sözlük **kapalı** · **DB'de saklı** baseline'a karşı bir owner **`deny sid=default`** alıyor (var olan tenant'lar güncellenmiyor) yani kapı **sahibi kilitlerdi** · ve `billing:close` için **guardrail yok**, izin baseline'da olsaydı **tenant ezebilirdi** — denetçi `allow sid=tenant:let-managers-close` ile **kanıtladı**. **Ders: bir emsal ancak MEKANİZMASI da taşındığında emsaldir; "M6-09 böyle yaptı" bir gerekçe değil, bir hipotezdir.**
>
> **🔴 İKİ ORKESTRATÖR KARARI, İKİSİ DE GÜVENLİK MERCEĞİNİN ÖLÇÜMÜNDEN DOĞDU:** bölüm **owner-only, okuma dahil** (bir manager işletmenin Tappa'ya borcunu okuyabiliyordu; Faz A ticari şartları uygulama rolüne **iki fiilde de** kapatmışken okumayı açık bırakmak duruşu yarım bırakıyordu) · ve CSV `billing.exported` **yazacak** (kardeş rota yazıyordu, `743/743/744` ölçüldü — sebep gizlilik değil **tutarlılık**).
>
> ⚠️ **FİLTRE NAVİGASYONA KONDU, ROTA TABLOSUNA DEĞİL.** `mountSections` hâlâ tam listeyi geziyor → manager **handler'dan 403** alıyor, chi'dan 404 değil, ve *"linki 404 veren sekme"* **yapısal olarak imkânsız** kalıyor. Filtreyi rota tablosuna taşıyan mutasyon **dört testi** kırmızı ediyor.
>
> **2932 PASS / 0 FAIL / 0 SKIP · 18 paket** · `make check` **exit 0** (commit sonrası, manifest `c972e74f…` önce=sonra) · `make audit` · `make lint` · `redline-check.sh` **ayrı ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M7-01**.

> **M6-12 FAZ A DONE — 2026-08-13 (`e085ae6`), MIGRATION 00016, 5 TUR / 3 RED.** İki **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**. `billing_periods` (dondurulmuş fatura satırı) + `tenants.price_per_employee_month` + 4 sorgu + `internal/domain/billing`. **Ekran/CSV/founding uyarısı/kapatma rotası = FAZ B.**
>
> **🔴 ORKESTRATÖRÜN ÖLÇMEDEN YAZDIĞI BİR ATIF ÜÇ YERE İŞLENDİ.** Devir notum *"kirlilik seed'de **ve** test fixture'larında"* diyordu; denetçi ölçtü: **seed sıfır çelişen satır üretir** (`seed.sql:225-227` damgayı statüden türetiyor), çelişenlerin **hepsi** `seedflow_db_test.go:413`'ten, sayı **kalıcı değil** (koşu başına +1), ve **hiçbir ürün yolu bu satırı yazamaz**. Yüklem korundu, gerekçe **"düzeltici" → "SAVUNMACI"** oldu. **Ders: ölçülmemiş bir devir cümlesi, migration yorumuna ve plan kartına kadar yayılır.**
>
> **🔴 KARTIN İSTEDİĞİ CHECK KISITI ÖLÇÜMLE ELENDİ** — ve elemenin en güçlü terimi rakam değildi: CHECK, **kuralı kanıtlayan fikstürü** reddederek kuralı **test edilemez** kılardı. ⚠️ İlk ölçüm düşük çıkmıştı çünkü iki yarım **birlikte** koşturulmuş, kaskad hatalar ucuzunu **maskelemişti**.
>
> **🔴 YAPICI BRIEF'TE OLMAYAN BİR ZAYIFLIĞI KENDİ BULDU:** §4.5 ağı **kendi SQL'ini reddetti** (tenant geçişli bağlanıyordu). Yeniden yazdı; denetçi **aritmetiğin değişmediğini** (altı ay birebir) ve **kemerin kozmetik olmadığını** (süper kullanıcıyla: kemerli 3, kemersiz 43) ölçtü.
>
> **🔴 GÜVENLİK MERCEĞİ: daraltma UPDATE'i kapatıp INSERT'i açık bırakmıştı** — ve **yapıcı denetçinin düzeltmesindeki eksiği yakaladı**: sütun listesinde `id` yoktu, onsuz her INSERT `WITH CHECK`'e takılır ve **signup tamamen kırılırdı**. `created_at` de kapatıldı (kendi kayıt tarihini yazan tenant bedava aylarını kaydırır).
>
> **2865 PASS / 0 FAIL / 0 SKIP · 18 paket** · `make check` **exit 0** (commit sonrası, manifest `d6d8d8bb…` önce=sonra) · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M6-12 FAZ B**.

> **M6-11 DONE — 2026-08-12 (`f39a341`), 3 TUR / 2 RED, ÜÇ FARKLI ÜÇÜNCÜ GÖZ + `tappa-security-auditor` ONAY.** `/admin/anomalies`, **8 salt-okuma sorgusu**, **yeni migration YOK**.
>
> **🔴 SEKİZ KRİTERİN ÜÇ YARISI KAYNAKSIZ ÇIKTI VE UYDURULMADI — DÜŞÜRÜLDÜ.** Üçü de ölçüldü, üçü de **ekranda ilan ediliyor**: *"POST'suz `GET /t`"* (kart biliyordu) · *"tek cihazdan çoklu aktivasyon"* (`sessions`'ta `created_ip` yok; `device_info` **7.430 oturumda 25 değer**) · ve *"hiç çapraz-lokasyon göstermeyen çalışan"* — **974 çalışanın 974'ünü** işaretliyor. **Bir kriterin ölçülüp düşürülmesi, sessizce yarım yapılmasından iyidir.**
>
> **🔴 KART MOTORUN MEKANİZMASINI YANLIŞ ANLATIYORDU.** Y-E *"IP eşleşti ama GPS uyuşmuyor"* diye tanımlıydı; `decide.go:715-722` bunu **yalnız mesafeden** türetiyor ve baseline politikası da **daraltmıyor**. Kart `decide.go` alıntısıyla düzeltildi.
>
> **🔴 BLOKLAYAN BULGU GERÇEK ARİTMETİKTİ.** Kod **iki yerde** *"snapshot'sız kayıt temiz sayılmaz"* derken üç oranın **paydası onları içeriyordu**, üstelik **iki farklı payda**. Düzeltme: `Answerable = Records − Unanswerable`, ve **hangi sayının hangi tabandan** olduğunu söyleyen bir cümle **her zaman** basılıyor.
>
> **⚠️ HIZ İDDİASI ÜÇ KEZ ÖLÇÜLDÜ, ÜÇÜ DE AYRIŞTI, SONUNDA GERİ ÇEKİLDİ** (209/221/252 → **17–19** → 103–110 → **276–290 ms**). Sebep ölçüldü: **üç farklı join stratejisi** ve **büyüyen pencere** (her `make test` içine yazıyor, §4.3 geri almayı yasaklıyor). Yerine **makineden bağımsız kesir** kondu — ve kapanış denetçisi **farklı makinede, farklı stratejiyle %83,9** ölçtü. **Ders: bir duvar saati bir sorgunun özelliği değildir.**
>
> **🔴 §4.7 DESEN AĞI COĞRAFYA KÖRÜYDÜ.** `\d{2,3}` **iki tam haneden az hiçbir dereceyi göremiyor** — Accra, Singapur, Londra, Paris; **Malta çalıştığı tek coğrafya**. Denetçi uçtan uca kanıtladı. Genişletme **önce ölçüldü** (üç sayfa şeklinde ondalık **0/0/0**) → bedel **sıfır** → genişletildi; kalan iki sınıf **sayıldı**, ve saymanın gerekçesi **maliyet değil kapsam** olarak yazıldı.
>
> **Güvenlik merceği ONAY:** hiçbir sorgu `gps_lat`/`gps_lng`/`source_ip` seçmiyor, `policy_context` **sınırı geçmiyor**, RLS beş yolda kapalı. İki ağırlık kararı: §4.2 *"hareket örüntüsü"* **sınır içinde**; `ListTapsTakenTogether` **yeni bir sosyal çıkarım yeteneği** → backlog **T25**.
>
> **2739 PASS / 0 FAIL / 0 SKIP · 17 paket** · `make check` fmt/gen ağacı **değiştirmiyor** (manifest kanıtlı) · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0 · `make simulate-day` PASS. **Sıradaki:** "ŞU AN" → **M6-12** (M6'nın son görevi).

> **M6-09 FAZ B DONE — 2026-08-12 (`2cf55f2`), MIGRATION 00015, 11 TUR / 10 RED — PROJENİN EN UZUN GÖREVİ.** `/admin/policies` **yazma tarafı**. **On farklı** üçüncü göz (her turda yeni) + `tappa-security-auditor` **iki kez, ikisi de ONAY**.
>
> **🔴 SEVK EDİLEN ÜRÜNE TEK KUSUR GEÇMEDİ.** On turun bulduğu her şey commit'ten **önce** yakalandı, ve **son iki tur 0 ürün mantık kusuru / 0 build kırılması** çıkardı → **birinci durma kuralı** ile kapatıldı, bir denetim turu daha açılmadı.
>
> **`policy:edit` ÜÇ GÖREV ERTELENDİKTEN SONRA GERÇEKTEN ZORLANDI.** Yol **ölçerek** seçildi: **(b)** `checkin.Service.StoredSet` = `forTenant`'ın **materialise ETMEYEN** hâli. **(a)** elendi — reddedilen bir manager POST'u append-only tablolara **9/9/9** yazardı. **(c)** elendi — kapıyı `if actor.Role=="owner"` yapmak **iki testi kırmızı** ediyor, yani motordan **farklı** cevap veriyor. ⚠️ Ve naif kapı **sahibi kendi ekranından kilitlerdi**: guardrail yalnız **non-owner**'ı reddeder, owner'ın izni **baseline belgesinde**.
>
> **🔴 MIGRATION 00015 — BİR KİLİT SANILAN ŞEY BİR BAKIŞTI.** Orkestratörün 5. turda istettiği ad-çakışması guard'ı klasik **oku-sonra-yaz** çıktı (§4.4'ün dersi, bedeli bu kez **geri alınamaz satır**: `policies` DELETE vermiyor) ve **eşzamanlı bağlantı başına bir ikiz** üretti (`racers=64 → 16 satır`, `NumCPU=16`). `UNIQUE (tenant_id, layer, name)`, kapsam **ölçerek** seçildi. **İndeksin taşıyıcı olduğu ayrı kanıtlandı:** guard **köreltilince** 64 racer **hâlâ 1 satır**, 63 reddin hepsi **23505'ten**.
>
> **⚠️ AĞ FAZ B'DE ALTI KEZ DAHA YENİLDİ (toplam 13) VE ÜRÜN ON ÜÇÜNDE DE TEMİZDİ.** En öğreticisi **HTML5 bogus comment** (`<!x … >`: tarayıcı yutuyor, sayaç `</div>` **sanıyor**). Kapatan **türetim** oldu: sayaç **tamamen kaldırıldı**, bölge `PolicyGuarantees`'in **izole render'ından** alınıyor — sarmalayıcı bileşenin **tek kök elemanı** olunca aralarına **hiçbir şey** giremiyor. Kalanlar **sayıldı**. 🔴 **Ve ağın DUVAR OLMADIĞI kayda geçti:** kapatılamazlık **şemanın ve motorun** işi — `layer='guardrail'` INSERT'i **superuser'a bile** takılıyor, en özgül+izin veren tenant kopyası karşısında motor **yine** `deny/sys:*` veriyor.
>
> **⚠️ İMZA KUSURU FAZ B'DE ON BEŞTEN FAZLA KEZ ÇIKTI; altı turun beşinde bloklayan oradan geldi.** Yeni bir sınıf doğdu: **bir düzeltme başka bir yerdeki ölçümü sessizce geçersiz kılıyor** — 7. turun **doğru** fixture düzeltmesi, kartın *"ölçüldü"* dediği falsifier'ı öldürdü. Ve **`make check` sekiz tur boyunca yanlış raporlandı**: `templ fmt` `policies.templ`'i değiştiriyordu, yani `check` **kaynağı kirletip** takılırdı — orkestratör ölçtü, artık **manifest önce == sonra** ile kanıtlanıyor.
>
> **İki TEST DOUBLE üretimden ayrışıyordu ve tam da sınandıkları özellikte** — ikisi de sözleşmeye eşitlendi. **Karşılanmayan dört kabul kriteri de kartta yazılı.** Güvenlik merceği (2. kez, sıfırdan): **§4'ün yedisi temiz**, 11 yeni sorgunun **11'i** §4.5 ağında (kör **0**).
>
> **2710 PASS / 0 FAIL / 0 SKIP · 17 paket** · `make check` **fmt/gen ağacı değiştirmiyor** (manifest kanıtlı) · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0 · `make simulate-day` PASS. **Sıradaki:** "ŞU AN" → **M6-11**.

> **M6-09 FAZ A DONE — 2026-08-12 (`6738687`), İKİ DENETÇİ ONAY YOK / DÖRT RED → HEPSİ KAPATILDI, 8 TUR.** `/admin/policies` **okuma tarafı**. Üç genel üçüncü göz + `tappa-security-auditor` + kapanış denetçisi. **Yeni migration YOK** (son 00014). Yazma tarafı **FAZ B**.
>
> **Görev denetim merceğine göre ikiye bölündü** (M6-06'da üç, M6-07'de iki parçada haklı çıkan ölçüt): **A = okuma** (üç katman · sürüm geçmişi · guardrail sırası · yetkilendirme bölümü) · **B = yazma** (aç/kapa · yeni sürüm · `resource` · audit · `policy:edit`).
>
> **🔴 GUARDRAIL'İN KAPATILAMAZLIĞI ÜÇ DUVARLA YAPISAL:** domain tipi **kontrol alanı taşımıyor** (reflect) · view tipinin **her alanı string** · ve bir test şablonun **her dalını** render edip inert olmayan her eleman/özniteliği kırmızıya çeviriyor. **Sıra** `policy.Guardrails()`'in döndürdüğü dilimden **türetiliyor**, elle yazılmıyor. Güvenlik merceği taklidi **kurarak** denedi: tenant belgesine `sys:sun-invalid`, `base:qr-requires-ip`, `ignore`, `redirect` → **dördü de `StateUnreadable`**, **0 statement** render, guardrail bölümü **değişmiyor**.
>
> **🔴 OKUMA HİÇBİR ŞEY YAZMIYOR — VE BU İDDİANIN KENDİSİ.** `checkin.forTenant` eksik baseline'ı **materialise ediyor**; panel o yolu **kullanmıyor**. Ölçüldü (statik çağrı grafı **ve** canlı sayım): baseline'sız tenant **0/0/0 → 0/0/0**, sağlıklı **9/9/9 → 9/9/9**, **bozuk belgeli 2/2/2 → 2/2/2**.
>
> **🔴 BLOKLAYAN BULGU EKRANIN VARLIK SEBEBİNE DOKUNDU.** Bir tenant'ın dokuz baseline belgesinden **biri** okunamazsa motor **guardrail'lere tek başına** düşüyor — ama ekran diğer **sekizini `In force`** diye ve onlardan türeyen **`Granted to`** izinlerini basıyordu: **ADR 0004'ün *"var olan en tehlikeli hata"*** dediği şey. ⚠️ **Yapıcının kendi yorumu bunu adıyla koymuştu** ve kusuru **o kural için** çözüp **yayılım alanı için** çözmemişti. Kapatıldı: okunamayan kural **adıyla**, hiçbir kuralda `In force` **yok**, yetki bölümü **aynı bayrağı** taşıyor. Ve bayrak **türetildi** — hangi durumun uyarıyı hak ettiği ölçülerek ayrıldı (`StateOff` **atlanıyor**, `StateNoDocument`/`StateNotProvisioned` **kendini onarıyor**, **`StateUnreadable`** `EnsureBaselinePolicyVersion` çakışması yüzünden **onarılamıyor**). Sonra: **kapalı+okunamayan** bir belge yanlış alarm veriyordu → `enabled` kontrolü **parse'tan öne** alındı, yani panel motorun ayrımını **taklit etmiyor, tekrarlıyor** (dokuz belgelik matriste **ayrışan girdi yok**).
>
> **⚠️ İNERT-MARKUP AĞI YEDİ KEZ YENİLDİ, DÖRT DENETİM BOYUNCA — VE ÜRÜN YEDİSİNDE DE TEMİZ ÇIKTI.** Kırılan hep **koruma** oldu. Kanallar: deny-list fazla dardı (`<a hx-post>`/`<details>`/`contenteditable`) · **düzeltmenin kendisi** ağın göremediği yere düştü (koşullu blok hiç render edilmiyordu **ve** çapa ondan sonraydı) · **dört koşullu bloğu hiçbir fixture sürmüyordu** (biri üretimde **her sayfada**, tam da *"no policy can widen this one"* cümlesinin dibinde) · **kabuğun kendi token sözlüğüyle** kurulan tam işlevli POST kontrolü · **ödünç tanık** · `switch`/düz-Go görünmezliği · ve bir **öznitelik değerindeki ham `>`** tarayıcıyı körleştiriyordu.
> **Kapatanlar ÜÇ TÜRETİM oldu:** fixture kümesi **`.templ`'in kendisinden** türetiliyor (ilk koşu **sekiz sürülmeyen dal** + **altı ayırt etmeyen tanık** buldu) · `panelShellTokens` **tamamen kaldırıldı**, yerine **bayt-tam eşitlik** (`outsideBefore + outsideAfter == shellOnly`) · ve tarayıcı okuyamadığı etiketi **reddediyor** (fail closed). **Kalanlar İKİNCİ DURMA KURALIYLA SAYILDI**, test dosyasının *"NE TUTAR / NE TUTMAZ"* envanterinde ve kartta: `switch`/düz-Go (**18 kol / 9 şablon — türetiliyor**) · ödünç tanık · kuyruk-öneki · kabuğa konan kontrol.
>
> **⚠️ VE "SAĞLAMADIĞI GARANTİYİ BEYAN ETMEK" SINIFI BU GÖREVDE DÖRT KEZ ÇIKTI** — sonuncusu en çarpıcısı: bir başlık *"her dalı TÜRETİR"* diyordu ve **iki satır altta** *"HER DALI TÜRETMİYOR, VE BAŞLIK ÖYLE DİYORDU"* yazıyordu — **başlık değişmemişti**. Yapıcı kendi limit cümlesini **iki kez olduğundan küçük** yazdı (`switch` *"üç şablonda"* → **9**), ikincisinde sayıyı **türeterek** kapattı.
>
> **GÜVENLİK MERCEĞİ:** §4'ün yedi maddesi **temiz** · §4.5 kemeri mutasyonla kırmızı, `created_by` çapraz-tenant **çözülemiyor** · §4.7 sayfada `csrf|token|hash|aes|secret|password|invite|bearer` **hiç geçmiyor** · kontrast **13,27 / 16,17 / 10,16 / 6,05 / 9,13** hepsi AA.
>
> **2657 PASS / 0 FAIL / 0 SKIP · 17 paket** · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M6-09 FAZ B**.

> **M6-08 DONE — 2026-08-10 (`03a95fa`), MIGRATION 00014, ÜÇ DENETÇİ ONAY + BİR RED, 5 TUR.** Genel üçüncü göz **ONAY** · `tappa-security-auditor` **ONAY** · düzeltme turunu doğrulayan üçüncü göz **RED** → düzeltildi. **Ürünün `transactions`'a İKİNCİ YAZICISI.**
>
> **Q18 burada kapanıyor.** Sistem çıkış üretmez; M6-07'nin listelediği açık kayıtlar **burada, bir insan tarafından** kapatılır. `channel='manual'` · `entered_by` **oturumdan** (formdan değil) · **`sun_valid = NULL`** (kart `false` diyordu — **yanlıştı**, sütun üç değerli ve *"bu kanal için değerlendirilmedi"* doğrusu) · trust **taban** puanı · `verdict='ok'` (`flag` olsaydı saat **`HoursAwaiting`**'e düşer, müdür açık kaydı kapatır ve **toplam yine eksik kalırdı** — Q18'in tersi).
>
> **🔴 GÖREVİN DÜRÜST YARISI: DÜZELTMENİN RAPORA NE YAPTIĞI ÖLÇÜLDÜ VE İYİ ÇIKMADI.** Eşleme motoru **en geç `in` + en erken `out`** alıyor → eklenen bir satır **yalnızca KISALTIR**. Yani **az ödemeyi onaran iki yön çalışmıyor**, ve daha kötüsü: bir `in` düzeltmesi **kapatılamaz bir açık kayıt** bırakıyor — `accumulate` öncekini `flush()` ile açığa düşürüyor, sonraki **her** `out` `open==nil` bulup `StartedEarlier++` yapıyor (**iki denetçi bağımsız üretti**: 7h / open=1 / startedEarlier **1→2→3**), ve müdür ekranda **iki kere yanlış** bir cümle okuyor. **Davranış DEĞİŞTİRİLMEDİ** (motor M6-07'nin sevk edilmiş davranışı); **ölçüldü**, müdürün commit'ten önce okuduğu **onay ekranına** yazıldı, ve **[ADR 0011](../adr/0011-duzeltme-satiri-yalnizca-kisaltir.md)** üç çıkış yoluyla kaydedildi (ADR 0009'un kardeşi, bu kez **parasal**). ✅ **Pozitif kontrol: ürünün ASIL senaryosu (unutulmuş çıkış) DOĞRU çalışıyor** — `in09` → `open=1`; müdür `out17` yazınca → **8h, open=0**.
>
> **🔴 MIGRATION 00014 BİR BARİYERİ YAPISAL YAPTI.** Şemada *"ok ⟹ yön"* vardı ama **tersi yoktu**, yani `verdict='reject', type='in'` **kabul ediliyordu** (üç denetçi de rollback'li sondayla kabul ettirdi, pozitif kontrolle). Artık `CHECK (verdict IN ('ok','flag') OR type IS NULL)`, **VALIDATED**, **0 ihlal / 333 481 satır**. **Pozitif biçim bilinçli** — ⚠️ ve gerekçesi **bir kez yanlış yazıldı, denetçi çürüttü**: *"`NOT IN` nullable sütunda sessizce bozulur"* iddiası ölçümle **yanlış** (iki biçim de `NULL OR FALSE = NULL` ile **aynı** bozuluyor); **gerçek** fayda **sözlüğe yeni bir verdict girerse**: `NOT IN` yönlü bir `'void'`'i **KABUL** (fail-open), `IN ('ok','flag')` **REDDEDİYOR** (fail-closed).
>
> **⚠️ VE ORKESTRATÖRÜN UYARISI İYİ YÖNDE ÇÜRÜTÜLDÜ.** *"Böyle bir satır rapora çalışılmış saat olarak girerdi"* — **yanlış**: güvenlik merceği ölçtü, **M6-07 A'nın `endpointState` fail-safe'i onu zaten karantinaya alıyor** (`reject`+yön → `worked=0 / awaiting=8h`). Yani bariyer **ikiden dörde** çıktı ve hiçbiri tek başına taşıyıcı değil: `decide.go:239`'un `if`'i **(çıplak invariant DEĞİL — `TestDecide_DirectionNilForNonRecordVerdicts`'in konusu, mutasyon dört alt vakada kırmızı)** · manuel yolun **SQL literali** · **`HoursAwaiting` karantinası** · **00014**.
>
> **KARARLAR (otonomi):** **policy motoru BAĞLANMADI** ve gerekçe **koda** yazıldı — panelde `Evaluate` çağıran handler yok, baseline **iki role de** veriyor, **ve `forTenant` baseline'ı MATERIALISE ediyor** (bir panel POST'u policy tablolarına **yazardı**); guardrail'in *"**authorized**"* kelimesi **silindi** · onay kapısı **gerekli** (telafi yolu az ödeme yönünde yok) · reddedilen yazma **200 + yeniden render** (not/tarih/saat/yön **korunuyor**; **çift yazma kurulup denendi, yazdırılamadı**) · `manager_entered_shifts` **iki yönde de yanlıştı** → aritmetiğe dokunulmadı, **ad ölçüme eşitlendi** (`manager_entered_arrivals`).
>
> **🔴 VE `make test` DETERMİNİSTİK YAPILDI (kapsam genişlemesi).** `TestPlaqueJourneyDB_TheBudgetOnATenantWithHistory` (M6-06 B) **kendi tenant'ını zehirliyordu** — koşum başına 2 plaket, kapaklı liste, demo tenant **287**'ye ulaşmış, **P(fail) ≈ %28,8 ve artıyor**. Bu, **bu kilometre taşındaki her *"suite yeşil"* iddiasını olasılıksal kılıyordu**. Düzeltildi: **8/8** tek-başına koşum, plaket deltası **+16 → 0**, üç tam koşuda sayı **287'de sabit**. `vat_number` çakışmasından sonra bu sınıfın **ikinci** vakası.
>
> **⚠️ TEK RED, VE SINIF TANIDIK: DÜZELTME TURU KENDİ DEĞİŞİKLİKLERİNİN TERSİNİ SÖYLEYEN DÖRT METİN BIRAKTI.** `checkin.go` *"veritabanı kabul eder"* diyordu (00014'ten sonra **reddediyor**) **ve var olmayan bir teste atıf veriyordu** · `manualentry_db_test.go` aynı yanlışı tekrarlıyordu, üstelik 00014'ün **kendi başlığının FALSE ilan ettiği** bir cümleyle · `manualentry.go` *"her sonuç 303 döner"* diyordu (**beş dal 200**) · ve 00014'ün pozitif-biçim gerekçesi (yukarıda). **Dördü de kapatıldı.**
>
> **2597 PASS / 0 FAIL / 0 SKIP · 17 paket** · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M6-09**.

> **M6-07 FAZ B DONE — 2026-08-10 (`3930a52`), ÜÇ DENETÇİ ONAY, 4 TUR, 0 RED.** CSV export. Genel üçüncü göz **ONAY** · `tappa-security-auditor` **ONAY** · düzeltme turunu doğrulayan üçüncü göz **ONAY**. **Yeni migration YOK, YENİ SORGU YOK** — `db/queries/` ve `internal/store/` **hiç dokunulmadı**, yani `EXPLAIN` gerekmedi. **A ile birlikte M6-07 = 8 tur, 0 RED, ALTI DENETÇİ.**
>
> `GET /admin/reports.csv` — ekranın render ettiği **AYNI `ledger.Report`**'u render ediyor; ikinci toplama **yok** ve ikisi ayrışırsa bir test düşüyor. Ekranın satır tavanları **uygulanmıyor** (CSV'nin var olma sebebi 200'den büyük kadro), ama bordro figürünün **dışladığı her nicelik** rakamlardan **ÖNCE** basılıyor.
>
> **🔴 KAÇIŞ SINIRI UNICODE'UN KENDİSİNDEN GELİYOR, ELLE ÇİZİLMİŞ BİR LİSTEDEN DEĞİL.** Hücrenin başındaki **mark · format · control · `Other_Default_Ignorable_Code_Point`** dizisi `= + - @` testinden **önce** atlanıyor. Yol buraya **üç adımda** geldi ve **her adımı başka biri açtı**: (1) yapıcı kendi ağını kurarken **üründe gerçek bir açık** buldu (`unicode.IsSpace('\x01')` **false** → `"\x01=1+1"` **kaçışsız** yazılıyordu); (2) güvenlik merceği `IsControl`'ün **yalnız Cc** olduğunu ve **13 Cf runeunun çıplak geçtiğini** uçtan uca ölçtü — **ve öldürücü kanıtı buldu: `internal/session/manager.go:512` bu sınıfı ZATEN çözmüş ve o dosyanın yorumu formül nötralleştirmeyi ADIYLA M6-07'ye devretmiş; devralan taraf sınıfın YARISINI almıştı** (*kalıbın yarısını kopyalama* sınıfının **beşinci** vakası); (3) doğrulayıcı göz **57 vektörle** yendi ve `Mc`'yi dışarıda bırakan gerekçeyi (*"boşluk kaplar → saklanma yeri değil"*) **kendi kanıtıyla çürüttü** — `U+3164` HANGUL FILLER kategori **`Lo`** ve **sıfır genişlikte** render oluyor, yani o ölçüte göre içeride olmalıydı: ***"sınır ilkeli değil, aramanın durduğu yer."*** Çözüm ilkeyi **Go'nun shipped ettiği Unicode özelliğine** bağlamak oldu.
>
> **YANLIŞ ALARM MALİYETİ ÖLÇÜLDÜ, İDDİA EDİLMEDİ: 662 873 gerçek ad** (çalışan · lokasyon · departman · tenant) — geniş küme, dar kümenin kaçırdığı **tam olarak aynı 74 hücreyi** kaçırıyor (**delta 0**); ve 30 elle kurulmuş çok dilli ad (`İlhan` · `Şirin Çelik` · `Ħbara` · `żejtun` · `e`+`U+0301` · `مُحَمَّد` · `דָוִד` · `राम शर्मा` · `山田 太郎`) → **0 değişim**. ⚠️ **SÖMÜRÜNÜN YARISI ÖLÇÜLEMEZ** — makinede hesap tablosu motoru yok ve **üç denetim de aynı duvara çarptı**; *"hücre kaçışsız iniyor"* **ölçüldü**, *"formül çalışıyor"* **iddia edilmiyor**. Açık sınıflar (NFKC dışı homoglyphler · East Asian Width · fontta görünmez ama ODI olmayan rune'lar) **koda sayıldı**.
>
> **KARARLAR (otonomi, hepsi ölçümle):** **GET** — indirme bağlantı olarak yaşamalı ve `sameOriginGate` **yer iminden açılan** indirmeyi reddeder (`Sec-Fetch-Site: none` → red; yapıcı *"eski tarayıcılar"* demişti, denetçi **her tarayıcıda** olduğunu ölçtü) · **UTF-8 BOM VAR** (BOM'suz UTF-8 Excel'de `ħ ġ ċ ż` / `ı ş ğ ç` bozuyor — **ürünün iki pazarı**; azaltma: **ilk satır bilinçli olarak bir başlık CÜMLESİ**, sütun adı değil) · **audit `Record`, `RecordTx` değil** (`Hours` bir **okuma**; ondan `pgx.Tx` sızdırmak okuma yolunu yazma yoluna çevirirdi) · **dosya adında tenant adı YOK** (asıl risk Go'nun CR/LF'i boşluğa çevirip **tırnağı çevirmemesi**; regexp **gerçek bir girdide ateşledi** — `week=0000-01-01` yıl 0'ın haftası `-0001-12-27`'ye düşüyor) · **`report:export` motora BAĞLANMADI ve bu RAPORA DEĞİL KODA yazıldı** (panelde `Evaluate` çağıran handler **yok**, baseline eylemi **iki role de** veriyor → rol kapısı kimseyi reddetmezdi; gerçek kapı **M6-09**).
>
> **⚠️ İKİ DENETÇİ BİR SAYIDA ÇELİŞTİ VE YAPICI ÇÖZDÜ — imza dersinin DENETÇİDE tekrarı.** Genel göz *"eşiğe **869 satır**"* dedi (ham `type IN ('in','out')`); güvenlik merceği sorgunun **gerçekten okuduğu pencereyi** ölçtü. Yapıcı `ListWorkedShiftEvents`'in birebir `WHERE`'iyle dört varyantı çıkardı, doğrulayıcı göz **dördünü de bağımsız SQL'le** üretti: **practice filtresiz/kuyruksuz %96 · practice filtresiz/kuyruklu %111 · `NOT practice`/kuyruksuz %54 · `NOT practice`/kuyruklu (BU SORGU) %63**. **Farkın tamamı `NOT t.practice`.** Genel gözün sayısı bir **popülasyon etiketi hatası**; orkestratörün `1,64×`'i **yanlış değil, BAYAT** → bugün **1,58×**.
>
> **GÜVENLİK MERCEĞİ (ONAY) — §4.2'yi YAPISAL olarak kapattı:** `ledger` tip grafının **tüm alanları** döküldü — `Trust`, `IPMatch`, `GPS*`, `SourceIP`, `Distance`, `PolicyContext`, `EnteredBy`, `Note` **hiçbiri yok**, yani koordinatın seyahat edebileceği bir alan olmadığı için **türetilmiş sızıntı da imkânsız**. Ayrıca: `?week=` ile **dokuz** düşmanca deneme, hiçbiri `Content-Disposition`'a ulaşamadı · anonim indirme → login, **audit satırı yazılmıyor** · çapraz-origin **gövdeyi okuyamıyor** (`Access-Control-Allow-Origin` **yok**) · `transactions`'a UPDATE/DELETE `tappa_owner` için de reddediliyor.
>
> **2493 PASS / 0 FAIL / 0 SKIP · 16 paket** · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M6-08**.

> **M6-07 FAZ A DONE — 2026-08-10 (`671289b`), ÜÇ DENETÇİ ONAY, 4 TUR, 0 RED — projenin İLK sıfır-RED görevi.** Genel üçüncü göz **ONAY** · `tappa-security-auditor` **ONAY** · düzeltme turunu doğrulayan üçüncü göz **ONAY**. **Yeni migration YOK** (son: 00013). **CSV export FAZ B**, ayrı tur.
>
> **Reports sekmesi:** kişi başına haftalık saat + günlük kırılım · lokasyon kırılımı · geç kalma **çalışanın KENDİ vardiyasına** göre · ve **toplama girmeyen** açık girişler kendi bölümünde. **Görev orkestratör kararıyla A/B bölündü** (ölçüt kapsam değil **denetim merceği**: A = aritmetik/§4.5/§4.6/§4.7 · B = çıktı yüzeyi/enjeksiyon/toplu veri çıkışı) ve **B sekiz yükümlülük** miras alıyor.
>
> **🔴 SIFIR RED, AMA DOKUZ + ÜÇ BLOKLAMAYAN BULGU — ve bunların ONU imza sınıfıydı: BİR AĞIN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİA.** Üründe **tek sızıntı yok**; yanlış olan **yazılı kapsamdı**. En ağırları: **§6 float ağı** yalnız `float64` **kelimesini** arıyordu → denetçinin gerçek akümülatörü (`ph.Worked.Seconds()*1e9 + …`) **tüm paketi yeşil** bıraktı (genişletildi: erişimciler + `token.FLOAT`; **sayılmış sınır** `go/types` ister) · **§4.7 refleksiyon testi** tam eşleşmeli **dokuz ad**tı → `Coords`/`GPS`/`SessionToken`/`InviteCode` **geçiyordu**, ve düzeltme sonrası ikinci denetçi **`RemoteAddr`**'ı geçirdi (**Go'nun `source_ip` için kendi alan adı**; `address` vardı, **`addr` yoktu**) → alt dize + **CamelCase kelime-tam** iki mod, yanlış alarm **ölçülerek** (53 alan, tek çakışma `Late`↔`lat`; `Resource*` re-**source**'a takılıyordu, `position` **82 meşru kullanım** olduğu için bilinçli **yasaklanmadı**).
>
> **🔴 TEK DAVRANIŞ DEĞİŞİKLİĞİ FAIL-SAFE'Tİ.** `endpointState`'in `default` dalı **bilinmeyen her verdict'i ödenebilir** sayıyordu (`endpointState("void","")` = `"counted"`; reddedilmiş bir çift → `Worked=8h`), ve onu savunan yorum **şemada karşılığı olmayan** bir CHECK'e dayanıyordu — gerçekte tek kısıt **tek yönlü** (`verdict <> 'ok' OR type IS NOT NULL`); rollback'li sonda `verdict='reject', type='in'` satırını **kabul ettirdi**. Bugün ulaşan yol **yok** (dört yol ayrı ayrı kapalı; canlı 303.128 satırda **0 aykırı**) — **ama bariyer bir kod değişmezi, yorumun iddia ettiği şema kısıtı değil, ve M6-08 ikinci yazıcıyı ekliyor.** `default` → **`HoursAwaiting`** (§4.6: kayıt kaybolmaz **ve** sessiz onay da olmaz). **Değişikliğin hiçbir gerçek sayıyı değiştirmediği ölçüldü, varsayılmadı:** 3 tenant × 4 hafta = **12 raporun 12'si bayt bayt aynı**.
>
> **🔴 DEVRALDIĞIMIZ BİR BORÇ KAPANDI, KAPI ÖLÇÜMÜYLE.** `tap.Decide`'ın `MinutesLate`'i **kaydın anını değil sunucu saatini** ölçüyordu (geriye tarihli giriş → **−520**). Ölçüldü: `minutes_late` **sütunu yok**, template alanı yok, log yok, `policy.CtxTimeMinutesLate` **tanımlı ama `keys` map'inde hiç set edilmiyor**, ve lateness policy'den **sonra** hesaplanıyor (`:212-230` → `:256`) → hiçbir guardrail'e giremez. Düzeltildi; rapor `occurred_at`'ten **yeniden hesaplıyor** (§4.3 geri doldurmayı yasaklıyor). ⚠️ `day_db_test.go`'daki `want 17` **testi zayıflatmadı, sıkılaştırdı**: `abs(got-want) > 2` toleransı **tam eşitliğe** döndü, ve o yorum **HEAD'de zaten vardı**.
>
> **⚠️ ÜÇÜNCÜ KEZ AYNI EXPLAIN, ÜÇÜNCÜ KEZ FARKLI İNDEKS.** Blok iki kez bir indeks adı iddia etti, ikisi de çürütüldü; üçüncüsünü **bağımsız bir okuyucu AYNI GÜN** çürüttü (`_occurred_idx` yazılıydı, `_location_idx` çıktı). **İndeks adı bloktan tamamen silindi** — kalan şey plan **şekli** + ölçülen büyüklükler + tarih. *Dürüst cevap: "istatistiklere bağlı".*
>
> **GÜVENLİK MERCEĞİ (ONAY) — kanıtla geçen yedi çizgi:** dört çapraz-tenant sondası **pozitif kontrollü** (A/A **14 153** satır · B id'siyle 0 · yüklemsiz 0 · JOIN'den isim 0) · isim sızıntısı **kurularak denendi**, composite FK reddetti · `AdvanceTagCounter` **dokunulmamış** (`git diff` boş) · beş tabloda `relforcerowsecurity = t`, `tappa_app` `rolbypassrls = f` · sorgular `gps_lat/gps_lng/source_ip/policy_context/entered_by` **seçmiyor** · tek yeni `slog` yalnız `err` yazıyor · `endpointState("ok","rejected")` **erişilemez** (review yalnız `verdict='flag'`'e bağlanabiliyor; canlı **flag'e bağlı 18 460 / non-flag'e 0**).
>
> **2416 PASS / 0 FAIL / 0 SKIP · 16 paket** · `make audit` exit 0 + `redline-check.sh` **ayrı** exit 0. **Sıradaki:** "ŞU AN" → **M6-07 FAZ B (CSV export)**.

> **M6-06 DONE — 2026-08-10 · üç parça, `d010c1f` + `ba671b0` (migration 00013) + `4ec5e85` · TOPLAM 31 TUR, 12 RED, ALTI DENETÇİ — PROJENİN EN UZUN GÖREVİ.** Her parça ayrı ayrı `tappa-security-auditor` **ONAY**'ı aldı. **Hiçbir kusur sevk edilmedi.**
>
> **Üçe bölündü** (ölçüt kapsam değil **denetim merceği**): **A — Locations & Departments** (16 tur, 5 RED) · **B veri katmanı — migration 00013** (7 tur, 3 RED) · **B panel — Wall Tags** (8 tur, 4 RED). Bölünme kullanıcı kararıydı ve **ölçüm onu haklı çıkardı**: `locations.sql`/`departments.sql`'de **yazma sorgusu yoktu**, `tags`'ta **INSERT yolu yoktu**, ve üç parça **üç ayrı saldırıyla** denetlendi.
>
> **🔴 SEKİZ KULLANICI KARARI:** A/B bölünmesi · **envanter modeli** (anahtar panelden geçmez — Tappa encode edip yükler, panel yalnız bağlar) · **T8+T9 birlikte 00013'te** · **referanssıza Delete** (%15,9) · **ondalık virgül normalleştirmesi** (Malta/Türkiye; yapıştırma belirsizliği **ölçüldü ve asılsız**) · **C′** (audit izine dayalı silme bildirimi) · **`owner`-only silme** · **ret satırı `audit_log`'a**. ⚠️ **Ve oturum ortasında kullanıcı otonomi verdi** (*"önerilerin varsa sen otomatik onayla"*) — sorulan sekiz sorunun **sekizinde de** öneri seçilmişti. Sonraki **yedi karar** ölçülüp **uygulandı ve raporlandı**: rol kapısı · ADR gerekmiyor (koordinat yazan **1/27**) · R4 tarayıcısı genişletilmiyor · geçmiş `audit_log`'dan okunacak · replace/un-mount onay kapısı · **mount için geri alma yolu** (kapı değil) · ve `make test`'in flake düzeltmesi.
>
> **🔴 ON İKİ KUSUR KAPATILDI.** En ağırları: `NaN` bir enlemi **500**'e çevirip müdürün **sekiz alanını** çöpe atıyordu (ve `1e-400` **sessizce 0.0**'a düşüyordu — `0,0` **ikinci bir kapıdan**) · tavan ötesi lokasyonlar **hiçbir rotadan düzenlenemiyordu** ve üstüne **yanlış** bir cümle basılıyordu · reddedilen departman düzenlemesi müdüre **veritabanının bozuk olduğunu** söylüyordu · `?done=venue-deleted` **hiç olmamış** bir silmeyi ilan ediyordu · silme **ürünün ilk geri alınamaz yazımıydı ve rol ayrımı yoktu** · liste **çalışmayan bir `Open` bağlantısı** basıyordu · panelin geri alınamaz eyleminde **onay yoktu** · **mount geri alınamazdı** (yanlış duvara mount → panelden **hiçbir kurtarma**, yedek yoksa **hiçbir kontrol**) · panel müdüre **ham veritabanı eylem adını** basıyordu · ve bir **guardrail fail-open**'dı (`sys:tag-not-active` **denylist**'ti; **QR yolu açıktı** → stok plaketten `flag` kaydı).
>
> **⚠️ İMZA KUSUR SINIFI, YİRMİ DÖRT KEZ: BİR AĞIN/CÜMLENİN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİA** — ve bloklayanların çoğu buydu, **hiçbiri üründe sızıntı değildi**. Alt sınıfı **sayı-bayatlaması**, **on dört kez**; sonunda **mekanik bir kural** kondu (*kodun sahip olduğu bir kümeyi tarif eden çıplak bir tamsayı yorumlarda yer almaz*) ve **dört dizin süpürüldü: 169 aday → 17 kod-sahipli yer, 11'i sayıyı SİLEREK kapatıldı**. Diğer alt sınıfı: **elle bakılan bir N listesi, N büyüyünce delik açar** (**yedi kez**) → sonunda **iki türetim** (eylem sözlüğü domain'in kendi bildirimlerinden, yazma rotaları **gerçek router'dan**) — ve rota türetimi **Faz A'nın dört rotasını da** kapsadı, ki onlar bu özellikle **hiç kapsanmamıştı**.
>
> **⚠️ ÜÇ DEVRALDIĞIMIZ CÜMLE ÖLÇÜMLE ÇÜRÜTÜLDÜ.** **T8'in öncülü** (*"mevcut satırlar zaten büyük harf"*) — gerçek **18.010** küçük harfli satır, **12.437'si append-only `transactions`'ın 24.874 satırından referanslı** → normalleştirme **§4.3 ihlali** olurdu, yapılmadı, kısıt **`NOT VALID`** sevk edildi. **T9'un trigger koşulu** (`> OLD`) `last_ctr`'a dokunmayan bir **retire'ı reddediyordu**. **T9'un sütun listesi** eksikti — *aynı günün iki kullanıcı kararı birbirini uygulanamaz kılıyordu.* **Ders: bir borcun ÖNERDİĞİ ÇÖZÜM de, ÖNCÜLÜ kadar ölçülmelidir.**
>
> **GÜVENLİK MERCEĞİ ÜÇ KEZ KOŞTU, ÜÇÜNDE DE ONAY.** Ölçtükleri: **altı çapraz-tenant sondası 0 satır** (A) · `AdvanceTagCounter` **bayt bayt aynı** (üçünde de) · **altı bypass kapalı** (`ON CONFLICT`, `MERGE`, `TRUNCATE`, `DISABLE TRIGGER`, `session_replication_role`) · **ctr sarması DoS değil** · **un-mount REPLAY PENCERESİ AÇMIYOR** (77 → 77 → 77; eşit/küçük ctr `UPDATE 0`) · **10 eşzamanlı un-mount → tam 1 kazanan** · çapraz-tenant **aktör adı sızıntısı kurularak denendi, sızmadı** · C′ oracle'ında beş sonda **bayt bayt aynı** (durum + başlıklar + `Content-Length` + `Set-Cookie` + sorgu sayısı) · jetonun **on iki parçası** teker teker, alan-sınırı kaydırması **inşa edilemez**.
>
> **2335 PASS / 0 FAIL / 0 SKIP · 16 paket** · migration **00013** · `make audit` exit 0. **Sıradaki:** "ŞU AN" → **M6-07**.

> **M6-06 B VERİ KATMANI done — 2026-08-09 (`ba671b0`), MIGRATION 00013, iki denetçi ONAY** (üç genel üçüncü göz, **üçü de RED**, artı `tappa-security-auditor` **VERDICT: ONAY**), **7 TUR**.
> `tags` sertleştirildi + **envanter modeli**. **Uygulama katmanı (panel) AYRI TUR** ve **on yükümlülük** miras alıyor (yukarıda "ŞU AN").
>
> **🔴 DEVRALDIĞIMIZ ÜÇ CÜMLE ÇÜRÜTÜLDÜ — üçü de backlog'da ve `state.md`'de YAZILIYDI.** **T8:** *"mevcut satırlar zaten büyük harf, veri taşıması yok"* → gerçek **18.010 küçük harfli** satır, ve **12.437'si `transactions`'ın 24.874 satırından referanslı**; `transactions` **DB düzeyinde append-only** (tetikleyici `tappa_owner`'ı da bağlıyor) → *"büyük harfe çevir"* = **tetikleyiciyi superuser'la kapatıp 24.874 delil satırını yeniden yazmak** = **§4.3 ihlali**. Yapılmadı; kısıt **`NOT VALID`** sevk edildi (yeni satırlar denetleniyor, artık satırlar **donuyor**, ve **tap yolu onlara ULAŞAMIYOR** çünkü `sun.Parse` daima büyük harf üretir + `char(14)` karşılaştırması harfe duyarlı — **kayıp kayıt yolu yok**, güvenlik merceği ayrıca aradı ve bulamadı). Kirlenmenin **kaynağı kapatıldı** (`ToUpper`'sız iki test yardımcısı; iki tam koşumdan sonra sayı **artmadı**). **T9:** trigger koşulu `NEW.last_ctr > OLD.last_ctr` **yanlıştı** — `last_ctr`'a dokunmayan bir **retire'ı reddediyor** (canlı hata mesajıyla gösterildi); doğrusu `< OLD` ise `RAISE`. Ve **sütun listesi eksikti**: `location_id` yoktu, ama envanter modeli **bağlamayı** istiyor ve bağlama `location_id` UPDATE'idir — ***aynı günün iki kullanıcı kararı birbirini uygulanamaz kılıyordu.***
>
> **Sevk edilen:** `CHECK (uid ~ '^[0-9A-F]{14}$')` NOT VALID · `REVOKE UPDATE` + **beş sütunluk** GRANT (`location_id, last_ctr, status, retired_at, replaced_by`) + monotonluk trigger'ı (`tappa_owner`'ı da bağlıyor) · **`location_id` nullable** + yeni durum **`unassigned`** + **üç CHECK** (`lost` **bilerek** dışarıda — stokta kaybolmuş plaket gerçek) · **T11 indeksi** `transactions(tenant_id, location_id)` · Faz A'nın devrettiği **ad CHECK'leri** (validated; `locations` 140.331 satır / **0** ihlal, Malta rune'larıyla `char_length` vs `octet_length` **doğrulandı**) · beş sqlc sorgusu.
>
> **🔴 BİR GUARDRAIL FAIL-OPEN'DI VE KAPATILDI (orkestratör kararı).** `sys:tag-not-active` bir **DENYLIST**'ti (`retired || lost`) ve 00013'ün eklediği `unassigned` **altından geçiyordu** → stok plakete tap **§5 satır 1'e hiç çarpmıyordu**. Güvenlik merceği zinciri sürdü: NFC `sys:sun-invalid` ile kapalı (**ama gerekçe metni yanlış**), **QR AÇIKTI** → `flag`, onay kuyruğuna kayıt; yani *"stok kutusundaki monte edilmemiş bir plaketin uid'ini okuyan çalışan, evinden müdürün onaylayabileceği kayıtlar üretebilir"*. ⚠️ **Migration kendi riskini FAZLA yazmıştı** (*"can end as `ok`"*) — `ok` **erişilemez**, kanıt yok, en kötü **`flag`**, **§4.6 korunuyor**. Guardrail artık **allow-list** (`!= active`) ve **eşdeğerlik ölçüldü, iddia edilmedi**: test durum sözlüğünü **migration'ın CHECK kümesinden türetiyor** ve iki formu **dört değerin dördünde** karşılaştırıyor. **Kazanç: beşinci durum, kimse hatırlamasa da eklendiği gün reddedilir.**
>
> **⚠️ BİR DÜZELTME, AYNI DOSYADAKİ BAŞKA BİR CÜMLEYİ GEÇERSİZ KILDI — yeni bir şekil.** 4. turda eklenen `tags_retired_keeps_its_location`, `Down` bloğunun kısıt **sayısını** (7→8) **ve** belgelenen kurtarma **reçetesini** aynı anda bozdu: reçete operatöre **şemanın kendisinin yasakladığı** bir yolu tarif ediyordu (`UPDATE`'te 23514, `Down`'a hiç ulaşmadan). Ders dosyaya yazıldı: *bir kısıt eklerken, o dosyanın kısıt SAYAN ve kısıt DAVRANIŞI TARİF EDEN her cümlesini yeniden ölç.*
>
> **⚠️ VE `make test` GÜVENİLMEZDİ — kapsam genişletmesi, onaylı.** `newPanelHarness` `vat_number`'ı uuid'nin **sekiz hex hanesinden** üretiyordu; dev DB o 32-bit uzayda **128.553** değer tutuyor → çakışma **koşum başına %1,13 (~89 koşumda bir)** ve **artıyor**. **19 dosyada 21 yer** düzeltildi → **9,2e-30**. Bu, oturum boyunca yapılan her *"PASS sayısı birebir tuttu"* doğrulamasının zeminiydi.
>
> **GÜVENLİK MERCEĞİ (ONAY):** `AdvanceTagCounter` **const ve func bayt bayt aynı** · yarış **50 goroutine × 5 tur, `-race`, taze DB'de** tam 1 kazanan · **altı bypass kapalı** (`ON CONFLICT DO UPDATE` 23001/42501 · `MERGE` 23001 · `TRUNCATE` 42501 · `DISABLE TRIGGER` *must be owner* · `session_replication_role` *permission denied*) · trigger `SECURITY INVOKER`, `search_path` **pinli**, `pg_temp` yüzeyi yok · **ctr sarması DoS DEĞİL** (sayaç yalnız `CMACVerified && active` iken ilerler) · beş çapraz-tenant sondası **pozitif kontrollü 0 satır** · taze migrate+seed'de **12 plaketin 12'si 44 baytlık zarf** ve `VALIDATE CONSTRAINT` **başarılı** → üretimde kısıt **fiilen validated doğuyor**.
>
> **SAYILMIŞ LİMİTLER:** `tappa_app` `tags`'ta **tablo geneli INSERT** tutuyor (*"the honest weak point"*; yükleyici gelince REVOKE) · **`Down` bir güvenlik geriye-gidişidir** (v12'de dokuz sütunda UPDATE geri geliyor, trigger düşüyor — **adı kondu**) · **R4 tarayıcısı satır-yerel** (çok satırlı yazım ağı atlıyor; **genişletilmedi** çünkü `rg -U` **11 blok / 0 gerçek ihlal** ve yanlış alarmların içinde **`AdvanceTagCounter`'ın kendisi** var — orkestratör kararı) · **`FORCE ROW LEVEL SECURITY` davranışsal olarak test EDİLEMEZ** (yalnız tablo sahibini bağlar, sahip superuser) → **katalog iddiası** doğru araç, ve öyle yazıldı.
>
> **2215 PASS / 0 FAIL / 0 SKIP · 16 paket** · `Down` **taze klonda** koşuldu ve geri alınmış şema **fresh v12 ile alan alan IDENTICAL**. **Sıradaki:** "ŞU AN" → **M6-06 B uygulama katmanı**.

> **M6-06 A FAZI done — 2026-08-09 (`d010c1f`), iki denetçi ONAY, 16 TUR, 5 RED — projenin EN UZUN görevi** (M6-01 B 18 tur/8 RED'di ama bu görev **beş ayrı üçüncü göz** gördü). `tappa-security-auditor` **VERDICT: ONAY**.
> **Locations & Departments sekmesi:** lokasyon CRUD (ad · `static_ips` `cidr[]` · GPS `numeric(9,6)` · vardiya · `overnight` · `wifi_ssid`) · departman yönetimi · **referanssıza silme** · **C′ audit-destekli silme bildirimi**. **Yeni migration YOK** (son: 00012) — `tappa_app` iki tabloda da DELETE'e zaten sahipti ve RLS politikaları `FOR ALL`; **varsayılmadı, ikisi de ölçüldü**.
>
> **🔴 GÖREV BÖLÜNDÜ ve `tags` FAZ B'YE KALDI.** Ölçüt kapsam değil **denetim merceği**: A = lokasyon/departman (§4.5 · §4.6 · yetkilendirme), B = plaketler (§4.7 AES · §4.4 replace tag) **ve migration 00013'te T8+T9**. Kullanıcı kararı 2026-08-08. **Kartın "encoded/pending" kriteri bugün TEMSİL EDİLEMİYOR** — `aes_key_ref bytea NOT NULL`, yani anahtarsız bir tag satırı **var olamaz**; B bunu **envanter modeliyle** çözecek (kullanıcı kararı: plaketi Tappa encode edip yükler, panel yalnız **bağlar**).
>
> **🔴 ALTI KUSUR KAPANDI, DÖRDÜ ÜRÜNÜ DOĞRUDAN VURUYORDU.** (1) `parseGPS` **`NaN`'ı geçiriyordu** — `NaN < -90` ve `NaN > 90` **ikisi de false**; Postgres 23514 veriyordu, müdür *"panel kullanılamıyor"* görüyordu ve **yazdığı sekiz alanın hepsi çöpe gidiyordu**. ⚠️ Bitişiğinde `1e-400` **sessizce 0.0'a** alt-taşıyordu — yani `0,0` Gine Körfezi koordinatı **ikinci bir kapıdan** geliyordu. (2) `venueForms` **kesilmiş dilimi** tarıyordu → tavan ötesi satır **hiçbir rotadan düzenlenemiyordu** ve üstüne *"bu işletmenin değil"* basılıyordu — **yanlış** (satır o işletmenin, RLS kabul ediyor). **M6-05 A'nın §4.6 sınıfının tekrarı.** (3) Reddedilen **departman düzenlemesi** *"Venue could not be read"* basıyordu: düzenlemede `location_id` **hiç POST edilmiyor** (`VenueFixed` kontrolü render etmiyor) ama arama POST'tan çözmeye çalışıyordu → **adını yanlış yazan müdüre veritabanının bozuk olduğu söyleniyordu**. İkinci yarısı daha ağır: create yolunda açılır liste **kesilmiş listeden** kuruluyordu, tavan ötesi venue kaybolunca **başka biri seçiliyor** ve eklenen departman **sessizce yer değiştiriyordu**. (4) `?done=venue-deleted` **hiç olmamış bir silmeyi ilan ediyordu** — ve fonksiyonun yorumu *"başlıklar DURUM adlandırır, olay değil, **bu dört**"* diyordu (dal sayısı **altı**). **M6-05'in RED alıp iki kuralla kapattığı sınıfın birebir tekrarı.** (5) Çağrı-grafı ağı **paket** kapsamı iddia edip **tek dosya** tarıyordu — denetçinin `locationactions.go`'ya koyduğu gerçek sızıntı (müdürün venue adındaki virgülü sessizce noktaya çeviriyor) paketi **160,5 sn yeşil** bıraktı. (6) Silme **ürünün ilk geri alınamaz yazımıydı ve rol ayrımı yoktu** — şemada `role IN ('owner','manager')` **var ve dolu** (30.359 owner / **7.417 manager**, **2.814 tenant'ın ikisi de var**), panel yazma yollarında rol okuyan **tek satır yoktu**.
>
> **⚠️ İMZA KUSUR SINIFI, ON BİR KEZ: BİR AĞIN/CÜMLENİN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİA.** Bloklayanların çoğu buydu ve **hiçbiri üründe sızıntı değildi** — yanlış olan **yazılı kapsamdı**. Örnekler: NaN düzeltmesi **yanlış satıra** atfedilmişti (muhafızı geri almak suite'i **yeşil** bırakıyor, çünkü regex NaN'ı daha önce reddediyor — *"kaldırılmasını hiçbir testin fark etmediği bir savunma, savunma değildir"*) · `no comma` testi bir **dizenin yokluğunu** pinliyordu, cümlenin **doğruluğunu** değil (mutasyonla *"commas are rejected"* yazan bir cümle **yeşil** geçti) · `08,00` alt-vakası **tam vakumdu** ve **davranışsal bir test onu asla göremezdi** (replacer iki nokta üretemez, `parseClock` iki nokta ister) → **çağrı-grafı ağına** çevrildi · ret satırının §4.7 taraması **yedi yasaklı kelimeye** bakıyordu, **oturum id'si hiçbirine çarpmıyordu** → **izinli anahtar kümesine** çevrildi · `created_at` ağı **anahtarı** tutuyordu, **değeri** değil (`0001-01-01` hem "var mı"yı hem `time.Parse`'ı geçiyordu).
>
> **⚠️ VE SAYILAR ÜÇ KEZ DOĞRU AMA POPÜLASYONU UYDURMAYDI.** *"Dört `EXISTS` = 8,154 ms"* yeniden üretilemedi (denetçi: **171–218 ms**) — sebep **öğretici**: `EXISTS`'lerin `OR`'u **kısa devre yapıyor** ve o tenant'ta **9 lokasyonun 9'unda çalışan** var, yani `transactions` **hiç değerlendirilmiyordu**; sıra değişince **~125 ms**. Düzeltmenin kendisi de yanlıştı (tenant filtresiz **tam tablo taraması**). Yapıcının cümlesi: ***"Her iki seferde de sayı doğruydu ve popülasyon uydurmaydı."*** Gerçek değişken **tenant'ın işlem hacmi** — aynı ifade **0,55 ms ile 242 ms**. Yeni kural üç yere yazıldı: *bir zamanlama, tenant'ı, satır sayısı ve işlem sayısıyla yazılır, ya da hiç yazılmaz.*
>
> **BEŞ KULLANICI KARARI + ÜÇ ORKESTRATÖR KARARI.** Kullanıcı: A/B bölünmesi · **envanter modeli** (anahtar panelden geçmez) · **T8+T9 birlikte 00013'te** · **referanssıza Delete** (%15,9 — 18.721 satır; referanslıda buton **hiç çıkmaz** ve nedeni sayıyla söylenir) · **ondalık virgül normalleştirmesi** (Malta/Türkiye virgül kullanır; yapıştırma belirsizliği **ölçüldü ve asılsız** — iki ayırıcılı her şey şekle takılıyor) · **C′** · **`owner`-only silme** · **ret satırı `audit_log`'a**. Orkestratör: **ADR gerekmiyor** (26→**27** `audit.Event{}` çağrı yerinden koordinat yazan **yalnız 1**; eşik **2/27** karta yazıldı) · uygulanmış migration'a **dokunulmadı** (00005'in çelişen yorumu `venue.go`'da **atıfla** uzlaştırıldı) · beş sayılmış limit **backlog'a** (T11–T14).
>
> **C′ — M6-05'in kuralı (2) HARFİYEN:** yönlendirme kaldırılan id'yi taşır, bölüm başlığı basmadan **aynı istekte** aktörün `audit_log` satırını okur (**tenant + actor + action + target + `at > now() - 30s`**, tamamı sunucu saati). Yabancının URL'si **hiçbir şey** basmaz. **0,101 ms**, `audit_log_tenant_at_idx` üstünde Index Scan, `Buffers: shared hit=3`, **migration yok**. Oracle değil: beş sonda **durum + tüm başlıklar + `Content-Length` + `Set-Cookie` + gövde baytı** olarak **bayt bayt aynı** (`BODYLEN 5455`), anti-vakum farklı sayfa üretiyor. ⚠️ **Okuma B (imzalı iddia) ÖLÇÜMLE ELENDİ**: jeton **tek kullanımlık değil**, yani kusuru kapatmaz **taşır** → backlog **T12**.
>
> **GÜVENLİK MERCEĞİ (ONAY) — kanıtla geçen yedi çizgi:** altı çapraz-tenant sondası canlı DB'de (**oku · düzenle · sil · departman ekle · referans say · silme izini doğrula**) → **altısı da 0 satır** · 11 yeni sorgunun **hepsinde** açık `tenant_id` · `tappa_app` NOBYPASSRLS, tablo sahibi değil, üç tabloda `relforcerowsecurity=t` · **beş resolver'ın hiçbiri** `locations`/`departments` okumuyor · **tap sorguları bayt bayt aynı** (`advanceTagCounter` 359 B) · `audit_log` append-only **`tappa_owner` için de** (tetikleyici) · 23503'ü **kendi tetikledi** · jetonun **on iki parçası** teker teker (alan-sınırı kaydırması **inşa edilemez**: action kapalı kümeden, uuid'ler sabit 36 bayt, `parse` tam 5 parça).
>
> **⚠️ YAPICI KENDİ HATASINI DOKUZ KEZ BİLDİRDİ** — ve sonuncusu ritmin kendisini kurtardı: mutasyon koşum aracının `Makefile.probe`'u `go test …; echo "EXIT=$?"` ile bitiyordu (`;` yüzünden make **daima 0**) ve yanlış paketi sabitliyordu → **iki mutasyon sahte YEŞİL**. Kendi çıktısındaki `--- FAIL` verdict'iyle çelişince fark etti, aracı yeniden yazdı (`$?` yayan, **derlemeyen mutasyona güvenmeyi reddeden**) ve dördünü de yeniden koştu: **hepsi KIRMIZI**. Diğerleri: uygulanmayan mutasyon · **çok zayıf** mutasyon (id kapısı bloklamış) · `&&` zincirinden sahte `exit 0` · **yalan söyleyen fixture** (`verify` her çağrıda taze `uuid.New()` oturum id'si → her owner yolu `confirm-required`; *"action/session bağının, yalan söyleyen bir fixture üstünde doğru çalışması"*) · kendi kırdığı `make audit` (§4.2'nin yasağını **alıntılayan** yorum R2 FAIL verdi → **tarayıcıya muafiyet eklemek yerine metni yeniden yazdı**) · ve **iki kez kendi yazdığı sayının yanlış popülasyondan geldiği**.
>
> **A kapanışında ölçüldü (2026-08-09): 2165 PASS / 0 FAIL / 0 SKIP · 16 paket · 98 test fonksiyonu (4 dosya) · kuşak ağı 18/60 (%30,0)** — ⚠️ **üçü de o günün sayısı, canlı değil**; B'nin veri katmanı bunları **2215** ve **24/65**'e taşıdı. `make audit` exit 0. **Sıradaki:** "ŞU AN" → **M6-06 B**.

> **M6-05 B FAZI done — 2026-08-08 (`77dcb92`), iki denetçi ONAY, 6 TUR, 4 RED (İKİSİ güvenlik merceğinden).**
> Aksiyonlar: **davet / yeniden davet · deaktive · lokasyon+departman değiştir**. Üç POST rotası
> `ProtectWriting` üzerinde + kişi başına aksiyon kartı. Her aksiyon `audit_log`'a **değişikliğiyle aynı
> transaction'da** yazıyor. **M6-05 KAPANDI.**
>
> **🔴 PROJENİN İLK CİDDİ GÜVENLİK KUSURU BULUNDU VE KAPATILDI — migration 00012.** Bir daveti harcamak
> **kardeş daveti emekliye ayırmıyordu**. HTTP üzerinden iki uçtan ölçüldü: iki basış → **2 canlı kod**;
> **en yenisiyle** aktivasyondan sonra **EN ESKİSİ HÂLÂ AKTİVE EDİYORDU** — ve ikinci-cihaz yolu
> `RevokeAllForEmployee` çağırdığı için **gerçek çalışanın telefonu oturumdan düşüyor**, elinde eski link
> olan **onun yerine trust 100 ile mesai yazıyordu**. **Kötü niyetli müdür gerektirmiyor:** gönder →
> *"gelmedi"* → tekrar gönder. ⚠️ **Mekanizma M5-02'den beri şemadaydı; onu ERİŞİLEBİLİR yapan şey bu
> görevdi** — davet kanalının **ilk üretim çağıranı** burada bağlandı ve *"iki kez bas"* tek tıklık bir
> müdür işlemi oldu. **Kusur eskiydi, erişilebilirliği yeni.**
>
> **⚠️ ŞEKİL MEKANİZMA DEĞİLDİR — ve bu kalıbın yarısını kopyalamak bu projede ÜÇÜNCÜ kez oldu.**
> Deaktivasyon onayı **dekoratifti** (onay GET'i hiç istenmeden POST → 303, deaktive) ve ürünün **tek geri
> alınamaz** aksiyonuydu. İlk düzeltmede çerez, alan ve sabit zamanlı karşılaştırma **vardı, anahtar yoktu**
> → denetçi **kendi çerezini basıp** geçti. `logincontext.go`'nun güvenliği **sunucu anahtarı altındaki
> HMAC**'ten geliyor. İkinci denemede `adminChoices`'ın parçaları **sayıldı** — ve sayı **on değil on bir**
> çıktı (gelecek-saat sınırı, denetçinin bulduğu). Önceki ikisi: M6-01 B'de `tap.go`'nun üç aşamasından
> **biri**, M6-04'te `sameOriginGate`'in **sırası**.
>
> **🔴 İKİ AĞIN ARASINDAKİ DELİK — yeni bir şekil.** Tüketen ifadeden `cancelled_at IS NULL` silinince
> **uçtan uca test yeşil** kaldı, çünkü `Lookup` iptal edilmiş kodu **daha erken** reddediyor. *"İki doğru
> katman, aralarında bir delik."* Yapıcı kendi buldu; ağ SQL katmanına kondu. Ve `ErrCodeCancelled`
> **hiçbir teste sahip değildi**: dalı silmek kontrolü `default:`e düşürüyor, o da **`failAttempt` olmadan
> 500** veriyordu → **sunulmuş bir devralma kimlik bilgisinin bıraktığı tek iz**, tek satırlık bir
> düzenlemeyle siliniyordu ve **tüm paket yeşil** kalıyordu (§4.6 kayıt kaybı). `switch` artık bir **tablo**
> ve sentinel kümesi **`go/ast` ile türetiliyor**; `default:` de **yazıyor**.
>
> **İki kullanıcı kararı (2026-08-08).** **Migration 00012** (kısa TTL yerine — TTL yalnız pencereyi
> daraltırdı, kapatmazdı) ve **onayın sunucuda zorlanması** (belgelemek yerine). ⚠️ Yapıcı kazancın
> büyüklüğünü **dürüstçe küçük** yazdı (aktör panelin kendi operatörü, GET-sonra-POST zaten yapabilir);
> güvenlik denetçisi **biraz büyüttü**: token sayfada, çerez `HttpOnly`+`SameSite=Lax`+`Path=/admin`, yani
> Origin kontrolü düşerse **ikinci katman senkronizatör token'ı** olarak duruyor.
>
> **SAYILDI, KAPATILMADI:** onayın **tek-atımlıklığı istemciye bağlı** — defter istemcinin çerezi, ve çerez
> silmeyi yok sayan bir istemci tek onayı **3 kez** harcadı. ⚠️ **Bunu gizleyen şey testin kendisiydi:**
> replay'i `browser` yardımcısı üzerinden yapıyordu ve **o yardımcı çerez silmeyi uyguluyordu** — yani
> assertion **sunucunun değil, test yardımcısının işbirliğini** ölçüyordu. Test artık **sunucuyu** ölçüyor
> ve **3** raporluyor. Üründe zararsız (2..N harcama `status <> 'deactivated'`'a çarpıyor, **hiçbir şey
> yazmıyor**). **Sıradaki:** "ŞU AN" → **M6-06**.

> **M6-05 A FAZI done — 2026-08-07 (`1998e89`), iki denetçi ONAY, 6 TUR, 4 RED (biri güvenlik merceğinden).**
> Employees sekmesinin **listesi**: ad · lokasyon/departman · durum · **oturum durumu** (canlı cihaz var mı, son
> kullanım) · keyset sayfalama **50**'de · ad/durum filtresi. **Yeni migration YOK.** ⛔ **Aksiyonlar B fazında
> ve bu diff'te YOK** — buton bile yok.
>
> **GÖREV BÖLÜNDÜ, ölçüt kapsam değil DENETİM MERCEĞİ** (agent-brief): okuma yolu §4.5/§4.6 ve **bayt maliyeti**
> ile denetlenir, yazma yolu §4.7/yetkilendirme/oturum ölümü ile. Ölçüm bölmeyi haklı çıkardı: `employees.sql`'de
> **yalnız iki sorgu** vardı ve **ikisi de tap yolundan** — okuma yolu M6-03'teki gibi **sıfırdan** yazıldı.
>
> **🔴 EN AĞIR BULGU GÜVENLİK MERCEĞİNDEN, VE İKİ GENEL TUR ÜSTÜNDEN GEÇMİŞTİ.** 512 rune'dan uzun bir ad
> **sayfa sınırına** düşünce imleç **sessizce düşürülüyor**, "Next page" 1. sayfayı yeniden veriyordu →
> **sonsuz döngü**. Ölçüldü: 60 kişilik kadroda 605 rune'luk ad → **10 kişi gezinerek ULAŞILAMAZ**, 50 kişi
> iki kez. `employees.full_name` şemada **sınırsız `text`**, CHECK yok. Ve **iki yazılı iddia ölçümle
> yanlışlandı**: *"düşürmek yalnızca DAHA FAZLASINI gösterir"* (tersi) ve *"filtre çubuğu etkin değerleri
> yankılar"* (imleç **hiçbir yerde** yankılanmıyor). ⚠️ Kusur bir kod hatası değil, **uygulanmayan bir
> varsayımdı** — `maxRosterCursorName`'in yorumu başarısızlık modunu **doğru teşhis edip** *"hiçbir bordroda
> böyle bir isim olmaz"* diyerek duruyordu; şemada CHECK, kodda validator, testte vaka **yoktu**. **Düzeltme
> sınırı büyütmedi, SİLDİ:** imleç artık yalnız `?after_id=<uuid>` taşıyor, adı sunucu çözüyor (**+0,44–0,99 ms**,
> yalnız sayfalı isteklerde) — ve bu **ikinci bir bulguyu da kapattı**: artık hiçbir çalışanın tam adı **URL'de
> yolculuk etmiyor**.
>
> **⚠️ BU FAZIN İMZA SINIFI: BİR AĞIN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİA.** §4.7'nin tip
> duvarı gerçekten çalışıyor (`Person`'a nötr adlı bir alan → KIRMIZI) ama *"bir önek de burada kapanır"*
> **yanlış**: `d.name AS department_name` takma adının altından geçen `left(token_hash,8)` **yeni alan
> istemiyor**, `Person` değişmiyor, ve **suite 16/16 yeşil** kalıyor. Üründe sızıntı **yok** (sevk edilen sorgu
> o sütunu seçmiyor); yanlış olan **ağın yazılı kapsamıydı** → *"COVERED ELSEWHERE"* silindi, kapsam **sayıldı**.
> Aynı sınıf üç kez daha: kuşak ağının kapsam iddiası **kendi bastığı komutla** çelişti (*"8/41"* → gerçek
> **9/43**) · bir ağın gerekçesi **var olmayan bir rota tablosu** beyan etti (*"mountWriting POST'un tek yeri"* —
> üç POST başka yerde, ve `/admin/logout` bir **mutasyon**) · ve `unscopedSubqueries` **21 saldırının 7'sinde**
> yenildi (`UNION` kolu · `JOIN…ON` ×2 · `public.sessions` · `ONLY` · virgül-join) → **kapanış kuralı**
> uygulandı: kapatılmadı, **adıyla sayıldı**, ve güvenlik denetçisi **RLS'in gerçekten tuttuğunu** canlı ölçtü
> (yedi şekil de yalnız kendi tenant'ının satırlarını döndürdü; `SET row_security = off` → **ERROR**).
>
> **📉 KADRO BOYU ALTI KEZ KAYDI VE HER TURDA DÜZYAZIDA ALINTILANDI** (8.718 → 9.138; bir denetimin **içinde**
> iki kez). İki tur üst üste **aynı** RED'i aldı çünkü düzeltme **yanlış katmandaydı** — üç sayı tazelendi,
> alıntılama alışkanlığı sürdü. Yapısal çözüm: **sayı hiçbir gerekçe cümlesinde yazılmıyor**, argüman
> **eşitsizlikten** kuruluyor (`RosterPageSize × adminSessionLimit ≥ rosterDesignCeiling`), ve bir **tel** şekli
> arıyor. ⚠️ Tel dürüstçe sınırlı: sözlüğü genişletmek **30 meşru ölçümü** işaretledi ve **geri alındı**
> (*yanlış alarm veren bir tel, bir sonraki kişinin sileceği teldir*); **altı kaçış** yazılı, ve **bütçe 9→6
> düştü diye borç azalmadı** — Türkçe ek (`çalışana`) yüzünden **ağın görüşü daraldı**, bu da yazıldı.
>
> **İki kullanıcı kararı (2026-08-07).** Sayfa **50**: 25'te bütçeyi aşan **tek tenant** kendi kadrosunu
> yürüyemiyordu (349 istek, **301'de 429**); 50'de **0 tenant** aşıyor, bedel sayfa **+%82** (22→40 KB, M6-03'ün
> kaldırdığı 867 KB'ın **1/21'i**). Ve **kart koda yenildi:** deaktivasyon **oturum İPTAL ETMEZ** —
> `employees.status` + `sys:employee-deactivated` reddi zaten veriyor; iptal etmek kişiyi **sonucun kesin olduğu**
> daldan (§5 satır 4) **her çağıranın dikkatine bağlı** bir dala taşırdı. **Sıradaki:** "ŞU AN" → **M6-05 B fazı**.

> **M6-04 done — 2026-08-07 (`2e7ec64`), iki denetçi ONAY, 9 TUR, 6 RED — M6-01 B'nin 18 turundan sonra
> projenin ikinci en uzun görevi.** Altıncı panel bölümü (`/admin/review`) ve **panelin ilk MUTASYON
> rotası**. Bir karar `transaction_reviews` + `audit_log`'a **tek transaction'da** yazılıyor ve
> `transactions`'a **hiç dokunmuyor** (Q20). **Yeni migration YOK** — 00005 tabloyu, `UNIQUE
> (transaction_id)`'ı, same-tenant bileşik FK'yi ve RLS beşlisini zaten taşıyordu.
>
> **🔴 KARTI ÖLÇMEK DÖRDÜNCÜ KEZ KENDİNİ ÖDEDİ, VE BU KEZ ESKİYEN ŞEY BENİM BRIEF'İMDİ.** *"`audit_log`
> YOLU HÂLÂ YOK"* notu (M5-11'den) **eskimişti**: `internal/audit` paketi **var** (`Record` = kendi
> transaction'ı, `RecordTx` = çağıranınkini paylaşır), ve dev DB'de **15 gerçek action'da ~20.000 satır**
> vardı. Doğru olan **dar** ifade: `channel='manual'` işlemini hedefleyen audit satırı **0** (bugün de
> **0**, sahibi **M6-08**) ve `review%` action'ı **0** — ikincisini **bu görev kapattı** (bugün **652**).
> Yapıcı brief'imdeki **iki sayıyı daha** ölçümle çürüttü: *"`transaction_reviews` 0 satır"* (gerçek
> **9.813**; o çıktıyı `head` kesmiş, ben görmeden yazmıştım) ve *"31.193 flag = kuyruk"* (o **DB geneli**;
> en büyük tek tenant **4.742**, ikincisi **30**).
>
> **🔴 EN AĞIR BULGU YİNE GÜVENLİK MERCEĞİNDEN GELDİ.** `sameOriginGate` oturum çözücüden **SONRA**
> koşuyordu, oysa `dashboard.go` *"logout'un kullandığı savunmanın aynısı"* diyordu. Ölçüldü: review
> **1 resolver okuması**, logout **0** — ve `adminlogin.go` bunu açık bir tasarım kuralı olarak yazıyor
> (*"a FREE refusal, BEFORE the resolver"*). `SameSite=Lax` gerçek cross-**site**'ı keser ama
> **same-site/farklı-origin** bir sayfa (alt alan XSS, https'in http ikizi) 300 POST attırınca **müdür
> 10 dakika kendi panelinden 429 alıyor** ve onay kuyruğunu temizleyemiyordu. ⚠️ **Bu, M6-01 B'de
> ORKESTRATÖRÜN yaptığı hatanın aynısıydı** — `tap.go`'nun **ByAddress → Identify → BySession**
> kalıbının yalnız bir aşaması kopyalanmıştı. Şimdi `ProtectWriting` = `floodGate → sameOriginGate →
> requireAdmin → sessionGate`, `Protect`'in **üst kümesi**, ve bir **resolver sayacıyla** pinli (yalnız
> status ile değil).
>
> **⚠️ BU GÖREVİN İMZA SINIFI: BİR AĞ, KENDİ MERKEZÎ CÜMLESİNİ TUTMUYOR — DOKUZ TURDA DOKUZ KEZ.**
> Karar formu kendi URL'ini yazıyordu ama yorum *"bölüm tablosundan okunuyor"* diyordu (hedefi
> `/admin/nowhere` yapan mutasyon **tüm paketi yeşil** bıraktı) · üç yorum bölümlerin `layout.Panel`
> render ettiğini söylüyordu, oysa **sıfır çağıranı** vardı ve *"yapısal"* denen script'sizlik bir
> **çalışma zamanı argümanıydı** · kuşak ağı `\.([A-Z]\w+)\(` regex'iyle **üç şekilde** yeniliyordu
> (satır sonunda nokta · parantez öncesi boşluk · metot değeri) — denetçi yüklemi **tamamen silip**
> `gofmt`-kararlı, `vet`-temiz bir yazımla **iki belt testini de yeşil** bıraktı · ve AST'ye taşındıktan
> sonra **okuyucusu** kaçırıldı: alfabetik olarak önceki bir dosyadaki **tek bir yorum satırı**
> sabitlenmemiş `-- name:` aramasını başka bir sorgunun gövdesine çözüyordu, `make sqlc` kapsamsız
> sorguyu **üretim koduna** yazıyordu ve suite **16/16 yeşil** kalıyordu.
>
> **İKİ KULLANICI KARARI.** (2026-08-06) Müdürün notu **write-only**'di — DB'ye gidiyor, hiçbir sorgu
> okumuyordu, yani 500 karakter kırpması **görünmezdi**; artık render ediliyor ve kırpma söyleniyor
> (sayfa 31.013 → 33.663 B tipik, en kötü 45.563 B; ⚠️ yapıcının 12,5 KB tahmini **iyimser yöndeydi**,
> gerçek **14.550 B**). (2026-08-07) Script'siz kabuk **yapısal** oldu: `pages.PanelShell` script
> parametresi **taşımıyor**, yani CSP'yi bir string düzenlemesiyle genişletmek artık **derleme hatası**.
> Kararı süren ölçüm: (b)'de tek testi nötralize edip düzenlemeyi yapınca **derlendi ve paket yeşil**,
> (a)'da **derlenmiyor**. `PanelShellWithScript` adını vermek hâlâ derleniyor ve yalnız bir testle
> yakalanıyor — **yazılı, kapatıldı denmedi**.
>
> **KAPSAM GENİŞLEMESİ (kullanıcı kararı 2026-08-07): `make check` artık `gen` koşuyor.** Bayat bir
> `_templ.go` ile commit edilen bir `.templ`, `make check`'ten **ve CI'dan geçiyordu** ve ürün eski
> markup'ı render ediyordu (ölçüldü). ⚠️ Yapıcı maliyet olarak **3,34 sn** bildirdi, sonra kendisi geri
> çekti: gerçek **10–15 sn** (`go run …@version` build cache'e bağlı) — *tek ölçümü nokta olarak yazmak*,
> sayı-etiketi sınıfının bu görevdeki **on birinci** örneği. Bu düzeltme `ci.yml:88-90`'ın yanlış
> iddiasını da **doğru hâle getiriyor**.
>
> **ADR 0009 — iki denetçi bağımsız olarak buldu:** verilmiş bir review **hiçbir yoldan düzeltilemiyor**
> (`UNIQUE` + `REVOKE` + trigger **`tappa_owner`'ı bile** reddediyor), yani §4.3'ün kendi telafi yolu
> bu tablo için **yapısal olarak kullanılamaz**. **İhlal değil** (korunan tap kaydı kusursuz), bilinçli
> (00005), ama **hiçbir dosya söylemiyordu**. **Sıradaki:** "ŞU AN" → **M6-05**.

> **M6-03 done — 2026-08-06 (`37032d0`), iki denetçi ONAY, 8 tur, 4 RED.** Günün tap'leri **docket kartı** olarak,
> **altı çalışan filtresi**, **keyset sayfalama** HTMX ile. **Okuma yolu sıfırdan yazıldı** — `transactions.sql`'deki
> beş sorgunun **hepsi tap YAZMA yolundandı**, yani panel ilk kez **gerçek tenant verisi** okuyor. Yeni migration
> **yok** (kartın istediği `transactions_tenant_occurred_idx` `00005:173`'te zaten vardı).
>
> **M6-02'nin devrettiği üç borç kapandı.** Filtre çubuğu **burada dürüstçe yazılabildi** (orada filtrelenecek veri
> yoktu). **HTMX bu görevle geldi** (2.0.10, gömülü, **CDN yok**, digest kayıtlı) ve **zorladığı CSP genişlemesini
> bu görev ödedi**: **iki direktif** (`script-src 'self'` + `connect-src 'self'`), ikisi de **gerçek tarayıcıda
> taşıyıcı ölçüldü** — ikincisi olmadan htmx **yükleniyor ama XHR'ı sessizce bloklanıyor**. **Tam bir bölüm**
> genişletilmiş politikayı gönderiyor ve **kardinalite pinli**; fragment **taban** politikayı alıyor.
> **Bütçe yeniden sayıldı ve çarpan GERİ GELMEDİ:** filtresiz görüntüleme hâlâ **1 ücretli istek**, yalnız
> sayfalama aşabiliyor — **43.400 gerçek tenant-gününde p99 = 2**. Türetme artık **bir isteğin DB ZAMANINI** da
> taşıyor: ilk sayımın **adlandırıp cevapsız bıraktığı eksen**.
>
> **🔴 SEVK EDİLEN FİLTRE ÇUBUĞU SAYFANIN %96'SIYDI.** Çalışan seçici **her çalışanı** `<option>` olarak basıyordu:
> **867 KB'lık sayfanın 835 KB'ı**, sınırsız büyüyen ve `no-store` yüzünden **her görüntülemede** yeniden inen.
> Kısa listeleme **ölçülüp reddedildi** (yalnız aktifler **%9**, *"son 90 günde tap'i olanlar"* **%0,3** azaltıyor;
> yalnız sert kesme işe yarar, o da **ayrılmış personeli filtrelenemez** kılar — §4.6). **Kullanıcı kararı
> (2026-08-06): metin kutusu + sunucu tarafı eşleşme** → sayfa **32 KB** ve kadro ne olursa olsun orada kalıyor.
>
> **⚠️ İKİ BULGU KAYDA DEĞER.** (1) **Kuşak ağının türetimi TEK DOSYA okuyordu** ama *"bu paketin kendi kaynağı"*
> ve *"çağrılan HER sorgu kontrol edilir"* diyordu; denetçi **pozitif kontrolle** kanıtladı: aynı kapsamsız sorgu
> `ledger.go`'da **kırmızı**, `ledger/extra.go`'da **yeşil**. Ve M6-04…M6-07'nin **dördü de** bu pakete okuma
> ekleyecek. (2) **`MATERIALIZED` CTE'nin performans gerekçesi YENİDEN ÜRETİLEMEDİ** — sebep: **veritabanı hiç
> `ANALYZE` edilmemişti** (`n_live_tup` 5.326, gerçek 111.167 → istatistikler **~20× küçük**). `ANALYZE` sonrası
> değiştirilen şekil **~4× DAHA HIZLI** çıktı. **27× geri çekildi**; çit **başka bir gerekçeyle** tutuldu:
> *maliyeti sınırlı, join filtresininki planlayıcıya bağlı ve tap havuzunu paylaşan bir yüzeyde 14 sn gözlendi* —
> ve bu, **karşı çıkanın bilerek geri alabileceği** biçimde yazıldı.
>
> **⚠️ İDDİA-ETİKETİ SINIFI SEKİZ KEZ ISIRDI VE MEKANİZMASI BULUNDU.** Sonuncusunda yapıcı üç düzeltmeyi
> *"yapıldı"* diye raporladı; ölçüm **2'de 1, 0'da 3, 0'da 2** dedi. Sebep: düzeltmeleri yapan betik **ilk
> `assert`'te ölmüş** ve yapıcı betiğin **çıktısını değil NİYETİNİ** raporlamış. En zararlısı bir yorumun
> *"vendored into `web/static/js/`"* demesiydi — vendor'u oradan çıkarmanın **tek sebebi** Tailwind'in taradığı
> ağaçtan kurtarmaktı, yani o yorumu izleyen kişi kapatılan kusuru **geri açardı**.
> **Sıradaki:** "ŞU AN" → **M6-04**.

> **M6-02 done — 2026-08-04 (`6757537`), üçüncü göz ONAY, 10 tur, 5 RED.** Panel kabuğu: `layout.Panel`,
> `TabBar` + `EmptyState`, üç CSS bileşen ailesi (`.btn`, `.empty-state`, `.tab-bar`) ve **tek bir tablodan**
> `Protect()` içinde mount edilen **beş sekme rotası**. `AdminHome` placeholder'ı gitti. Sekmeler **bilerek boş** —
> M6-03'ten itibaren doldurulacak.
>
> **🔍 KARTI ÖNCE ÖLÇMEK İKİ KEZ KENDİNİ ÖDEDİ.** (1) Docket motifi ve **beş** damga varyantı **zaten sevk
> edilmişti** — ve **M5-06'da değil, M0 iskeletinde** (`7e12f37`); M5-06 damganın **anatomisini** değiştirmiş,
> varlığını değil. Perforasyon görseli **hiç var olmamış**. Yani kartın üç kriteri **karşılanmıştı** ve iş,
> eksik olan **dört bileşendi**. Kartın damga listesi de eksikti (dört sayıyordu, **beş** var). ⚠️ **Yapıcı
> ORKESTRATÖRÜN brief'indeki tarihlemeyi de ölçümle çürüttü** — ve iki ayrı denetçi `git log -S` ile doğruladı.
> (2) **M6-01'in devrettiği borç ölçümle kapandı:** panel bütçeleri *"10 yönetici × 20 görüntüleme × ~10 HTMX
> parçası"* varsayımıyla türetilmişti; gerçek sunucuda **bir sekme görüntülemesi = TAM 1 ücretli istek**
> (bu görev HTMX **getirmiyor**, ve `/static` kapının **dışında** — bütçe harcanmışken 200 döndü). Adres
> tavanının payı **11,5× (3000/260)**, eski öncülün ima ettiği 1,46× değil. **Üç sabit de DEĞİŞMEDİ.**
>
> **🔴 SEVK EDİLMİŞ BİR KONTRAST HATASI BULUNDU VE DÜZELTİLDİ.** `.docket-label` **3,13:1** ile sevk
> ediliyordu (AA 4,5:1), **12 çağrı yerinde** — **çalışanın tap ekranı ve onay ekranı dahil** — ve beş ton
> daha **2,40–4,36:1** arasındaydı. **Kullanıcı kararı (2026-08-04): hepsi düzeltilsin** → `ink/70`
> (en kötü zeminde **5,58:1**). `/60` **ölçülüp reddedildi** (*"düzeltmeyen bir düzeltme"*, dört zeminde de
> kalıyor). Wordmark **WCAG 1.4.3'ün logotype istisnasını kullanabilirdi; REDDEDİLDİ** ve gerekçesi yazıldı.
> ⚠️ **Bağlayıcı zemin porcelain DEĞİL, `green-lite`** (L 0,8229 < 0,8627) — iki tur ve orkestratörün brief'i
> porcelain varsaymıştı çünkü **sayfa zemini** o; **türetilmiş test yakaladı**.
>
> **Ürünün İLK kontrast testi bu görevle geldi** ve üç `TestCompiledCSS_*`'in aksine **CI'da koşuyor**
> (`input.css` + `tailwind.config.js` okuyor, ikisi de commit'li): paleti config'den **yeniden türetiyor**,
> WCAG'i **yeniden hesaplıyor**, **sıfır çağrı yerinde koşmayı reddediyor**, ve **hangi zeminin bağlayıcı
> olduğunu** pinliyor (işaret silinirse de kırmızı: *"Removing the line is not a way to make this pass"*).
>
> **⚠️ BU GÖREVİN İMZA HATASI: SAYI HİJYENİ — BEŞ TURDA BEŞ KEZ.** Ve beşi de aynı kökten: **bir sayının
> ETİKETİ hangi büyüklüğü gösterdiği kontrol edilmeden yazılıyor.** En öğretici üçü: (a) düzyazı *"PORCELAIN
> IS THE BINDING GROUND"* derken **kendi tablosu ve kendi test dosyası** green-lite diyordu — **aynı tur**
> ikisini birden yazmıştı; (b) aynı cümlede **yük bir paydayla (260), pay başka bir paydayla (200)** →
> yazılan 15×, gerçek **11,5×**; (c) *"türetilen çağrı yeri sayısı"* diye basılan sayı **dosya sayısıydı**
> (5), gerçek **12** — ve **aynı satırdaki döküm 12'ye toplanıyordu**, üstelik bu **kanıt diye sunulan
> sayının kendisiydi**. **Sonuncusunu düzeltirken yapıcı kendi çift sayımını (24) kendi mutasyonuyla yakaladı.**
> **Sıradaki:** "ŞU AN" → **M6-03**.

> **M6-01 B fazı done — 2026-08-03 (`4bc2e72`), iki denetçi ONAY, 18 TUR, 8 RED — projenin en uzun görevi
> (M5-06'nın 15 turunu geçti).** Panel girişi uçtan uca: bcrypt (Q03) · admin oturumu · giriş ekranı ·
> "hangi işletme?" seçicisi · oran sınırı · `audit_log`. **Yeni migration YOK** — A fazının şeması gerçekten
> hazırdı. **Beş yükümlülüğün beşi karşılandı ya da dürüstçe limit yazıldı.**
>
> **Bu görevin öğrettiği tek şey var ve on sekiz turun sekizi onu tekrar etti: BİR DÜZELTMENİN KENDİ AĞI
> AYNI TURDA ÖLÇÜLMEZSE, DÜZELTME YAZILMAMIŞ SAYILIR.** Beş koruma sevk edildi ve **beşinin de silinmesi
> suite'i yeşil bıraktı** — `isLookupableEmail` · `sessionGate` · `sameOriginGate` · `meterOnly`'nin
> ücretlendirmesi · ve `CookiePath`/`maxCandidates`'in **totolojik** testleri (beklentiyi sabitin
> **kendisiyle** yazmak). Her biri ayrı bir turda, ayrı bir denetçi tarafından, **mutasyonla** bulundu.
>
> **🔴 En ağır bulgu güvenlik merceğinden geldi — genel üçüncü göz ONAY verdikten SONRA.** Q03'ün
> 72-bayt reddi (ki x/crypto'nun **canlı bir kusurunu** kapatıyor: `CompareHashAndPassword(hash(72×'a'),
> 100×'a')` **nil** döner) bcrypt'i **kısa devre** yaptırıyordu ve kukla yalnız *"hiç aday yok"* dalında
> ödeniyordu → **kayıtlı e-posta 5,53 ms, kayıtsız 295,42 ms = 53×**. Tek istekle, istatistiksiz,
> kimliksiz, ve **sunucuya maliyeti sıfır bcrypt**. 00011 bunu **OBLIGATION 2** olarak yasaklamıştı ve
> üç yerde *"kapalı"* diye yazılıydı. Düzeltme: uzun parola **aynı digest'e karşı** tam maliyeti öder.
> Ve düzeltmenin kendisi bir sonraki turda yakalandı — eklenen üçüncü `-short` skip'i kehanetin **iç
> döngüdeki tek savunmasını** sildi (delik geri açıkken `go test -short ./...` **14/14 yeşil**).
>
> **Üç bulgu daha, üçü de ölçümle:** `GET /admin` **hiç çerezi olmayan** bir çağırana bütçesiz bir
> `SECURITY DEFINER` resolver okuması ödetiyordu (uydurma 43 karakterlik token **1,36 ms** vs bozuk
> **156 µs**, 600 istekte **0×429**) · onu kapatmak için eklediğim flood kapısı **çıkışı reddedip oturumu
> canlı bırakıyordu** (kurbanın adres anahtarını paylaşan biri 200 isteği yakınca `POST /admin/logout` →
> **429**, `Revoke` **0**, ve sunucu tarafı süre olmadığı için o pencerede oturumu bitirecek **hiçbir yol**
> yok) — **bu benim kararımın regresyonuydu**, `tap.go`'nun **ByAddress → Identify → BySession** deseninin
> yalnız ilk aşamasını uygulamıştım · ve `sessionGate` **koruduğu maliyetin yanlış tarafındaydı** (429 alan
> istek resolver okumasını **ve** `TouchAdminSession` UPDATE'ini **zaten ödemiş** oluyordu; ölçüldü:
> reddedilen istekte bile `last_used_at` değişiyor).
>
> **Sayı hijyeni kendi başına bir bulgu sınıfı oldu — ALTI kez.** `make test-short` bandı **üç kez** dar
> yazıldı ve **üç kez** tutmadı; dördüncü denemede format değişti (**gözlenen aralık 51–74 sn** + `make
> test`'in taşıdığı *"gözlem kaydı, hedef değil"* uyarısı). 00011'in en büyük sayısı (`cost-10 ≈ 60–100 ms`)
> **~4× iyimserdi** çünkü sevk edilen digest'ler **cost 12** (367–372 ms) → 500 adaylık şekil 30–50 sn
> değil **~185 sn CPU**; uygulanmış migration değişmez, düzeltme kartta yaşıyor. 15× flood gevşetmesinin
> gerekçesi **yanlış kolda** ölçülmüştü (uydurma token 1,2 ms yerine canlı oturum **5,7 ms**, çünkü o kol
> bir de UPDATE yazıyor) → gerçek maliyet 4,5 sn değil **17,2 sn**.
>
> **On iki limit yazılı, kapatıldığı iddia edilmedi.** En önemlileri: digest-tarafı zamanlama kolu
> (bozuk/boş `password_hash` → **154–198 ns** vs kukla **297,9 ms** = ~10⁶×; **bugün erişilemez** çünkü
> `password_hash` yazan üretim yolu yok, ama **M6-05/M7-04/M7-02'de süresi doluyor** — kural: *`admin_users.
> password_hash` yalnız `adminauth.Hash` çıktısıyla yazılır*) · çıkış **30000** üçüncü-taraf isteğinde
> reddedilebilir (invaryant **zayıfladı**, yazılı) · `adminSessionLimit = 300` **kopyalandı, türetilmedi**
> (**M6-02'nin en acil borcu** — adres tavanından dar). **Sıradaki:** "ŞU AN" → **M6-02**.

> **M6-01 A fazı done — 2026-08-03 (`66d5442`), iki denetçi ONAY, 3 tur.** M6'nın ilk görevi, M5-02'nin
> **A/B kalıbıyla** bölündü (veri katmanı → auth+ekran) çünkü iş bir migration + resolver + kripto bağımlılığı
> + iki ekran + oran sınırı + audit'i **tek commit'e** sığdıracaktı. **Görevin ilk adımı yine kartı ölçmekti**
> ve yine kart eksikti — ama bu kez eksik olan **şemaydı**: 00006 *"resolver YOK: giriş tenant'ı biliyor"*
> varsayıyor, **hiçbir şey tenant'ı kurmuyordu**. Bu bir **kullanıcı kararı** gerektirdi (global çözümleme +
> tenant seçici). **İki bulgu kayda değer:** (1) `citext`'in `=` operatörü `public`'te, sabit `search_path`
> altında **görünmüyor** ve Postgres **hata vermeden** `text=text`'e düşüyor → kimlik doğrulama araması
> sessizce **harfe duyarlı**; (2) parola hash'i **çıplak `string`**'di ve **altı** basma yolu onu verbatim
> sızdırıyordu. **Ve en ağır madde denetimden çıktı:** aday↔parola bağı yazılmamıştı, ve onu azaltmak için
> önerilen DoS çaresi (*"ilk eşleşmede dur"*) yanlış uygulanırsa **tam olarak o atlatmayı** üretiyor.
> **Sıradaki:** "ŞU AN" → **M6-01 B fazı**, beş yükümlülükle.

> **M5-11 done — 2026-08-02 (`1a945fd`), iki denetçi ONAY, 2 tur. M5 KAPANDI.** Bu görev **sevk edilmiş
> kodda bir §5 ihlalini** düzeltti ve kusurun adı **bir cümleydi**: `decide.go` *"birincil koruma çağıranın
> sorgusudur, **ki practice'i dışlar**"* diyordu — **sorgu dışlamıyordu**. Düzeltme **tek satır**
> (`AND NOT t.practice`); yorum-dışı diff **1**. **İki şey bu görevi kayda değer kılıyor.** Birincisi:
> **kartı yazan bendim ve senaryom yanlıştı** — *"yeniden aktivasyon ikinci bir practice verir"* dedim,
> yapıcı ölçümle çürüttü (`isPracticeTap` `LastForPerson == nil` istiyor **ve** `activated_at` `COALESCE`'lu;
> üstelik bu **repoda zaten yazılıydı**). Gerçek erişilebilir yol **geriye tarihli `occurred_at`**, yani
> **M9-01 kuyruğunun ürettiği şekil**. İkincisi: **ADR'nin ilk hâli, düzelttiği hatayı kendi içinde
> tekrar ediyordu** — *"düzeltme yolu mekanizma olarak zaten var … + `audit_log`"* yazıyordu ve güvenlik
> denetçisi ölçtü: **408 manuel satır / 0 `audit_log` satırı / 0 HTTP rotası**. Yani *"bir cümle, sistemin
> vermediği bir şeyi beyan ediyor"* sınıfı **bu oturumun en pahalı sınıfı** olmakla kalmadı, **kendi
> düzeltmesinin içinde de yeniden doğdu**. **Sıradaki:** "ŞU AN" → **M6-01**.

> **M5-10 done — 2026-08-02 (`68acb81`), iki denetçi ONAY, 6 tur, 4 RED.** **Görevin ilk adımı kodu yazmak
> değil, KARTI ÖLÇMEKTİ** — ve kart büyük ölçüde eskimişti: M5-04'ten **önce** yazılmış, oysa M5-04 imzalı
> tap bağlamını getirmiş ve kartın istediği `first_seen_at`'i **MAC'in içinde** sunucu saati olarak zaten
> sağlıyordu. Kartın **migration + retention altyapısı** istediği yerde doğru cevap **13 satırdı**; tablo
> kullanıcı kararıyla **yapılmadı** çünkü ölçüldü ki koruma tarafında **sıfır** ekliyor (pencere `GET`
> anından ölçülüyor ve o anı **saldırgan seçiyor**). **Asıl ders bu değil ama.** Genel üçüncü göz üç tur
> denetledi ve **yalnız cümle hataları** buldu; `tappa-security-auditor` sonra **bu diff'in ürettiği bir
> §5 ihlali** buldu: bandı açmak `sys:tap-freshness`'i (#4) `sys:employee-deactivated`'in (#7) **önüne**
> koydu, yani deaktif bir oturum **3 dakika bekleyerek** güvenlik uyarısını düşürüyordu. Saldırı yok,
> sadece beklemek. **Ve aile taraması ikinci, daha eski bir örnek buldu** (`sys:occurred-at-bound`, tetiği
> daha ucuz: bir form alanı). ⚠️ **Bir kural, yazıldığı hatayı ELEMİYORSA kural değildir:** eski yerleştirme
> kuralı (*"sırayı bozmayan her yere konabilir"*) ölçüldü ve **hatalı sırayı da kabul ediyordu**; yerine
> **yapıyı** kontrol eden invaryant kondu. **Ve sabit listeli test bir AĞ DEĞİL, DEĞİŞİKLİK DEDEKTÖRÜDÜR** —
> yeniden sıralamayı yakalar, **eklemeye karşı çaresizdir**, çünkü kırmızı testin doğal onarımı listeyi
> güncellemektir ve bu **tam olarak yanlış hamledir** (denetçi sahte bir 11. guardrail'le kanıtladı: üç
> paket de yeşil kalıyordu). **Sıradaki:** "ŞU AN" → **M5-11**, M5'in son görevi.

> **M5-09 done — 2026-08-02 (`b0044c5`), iki denetçi ONAY, 3 tur, 1 RED.** Görev iki iş yaptı:
> **bilinen engeli kaldırmak** (seed'in `aes_key_ref`'i 42 baytlık düz ASCII'ydi → her seed'li plaket
> NFC yolunda **500**; zarf operatörün KEK'ine bağlı ve `Wrap` taze nonce çektiği için **SQL literali
> olamaz** → seed iki adımlı oldu) ve **bir günü gerçek HTTP + gerçek Postgres üzerinde üretmek**
> (10 çalışan, 31 kayıt, hepsi karar motorundan — yeni dosyalarda **sıfır** `INSERT INTO transactions`).
> **Görevin şeklini belirleyen şey ADR 0006'ydı:** debounce sunucu saatiyle ölçüldüğü için gün
> **sıkıştırılamıyor** (beklemesiz koşuda 15 kaydın 10'u `sys:person-debounce`, ve §4.6 gereği 15/15
> satır yine yazılıyor). **Motor fixture'a eğilmedi** — gün gerçek zamanda bekledi, ve bunun bedeli
> (62 sn) kullanıcıya sayılarla soruldu → **`make test` tam kaldı, `make test-short` eklendi**.
> **En değerli çıktı bir test değil, bir MOTOR HATASI:** yapıcı `practice` satırının daha eski bir
> **açık girişi maskelediğini** buldu, iki denetçi bunu **düz HTTP'den erişilebilir** olarak doğruladı
> (§5'in yön kuralı sevk edilmiş kodda ihlal ediliyor, **hiçbir sinyal vermeden**) → kullanıcı kararı:
> **M5-11 açıldı**. **Ve iki denetim aracı kendi kapsamlarından yakalandı:** `assertAfterShiftStart`
> **yapısal olarak boştu** (mutasyonla kanıtlandı: geç kalma hesabı tümden ölse bile yeşil kalıyordu),
> `assertTellableApart` **dejenere değer tuzağına** düştü (iki taraf da aynı damgadan türüyordu — tuzağı
> önlemek için yazılmış kontrolün içinde), ve **`make audit`'in `SRC`'si `test/`'i hiç taramıyordu**.
> **Sıradaki:** "ŞU AN" → **M5-10**, sonra **M5-11**.

> **M5-07 done — 2026-08-01 (`e0a5700`), iki denetçi ONAY, 2 tur.** Görevin **yarısı zaten hazırdı**
> (`practice` M4-06'da sunucu türetimli, TRAINING damgası M5-06'da) — işin ilk yarısı bunu **ölçmekti**,
> ve ölçüm **iki maskeli mutant** buldu: `gather`'ın practice guard'ı silinince suite **yeşil** kalıyordu
> (motor tarafı bağımsız kanıtlı olduğu için), ve mapper'ın `Practice` alanı **eşdeğer mutanttı**.
> **Bu, oturumda üçüncü kez:** *bir garanti A paketinde kanıtlanıp B'de tüketiliyorsa, B'nin onu
> KULLANDIĞI ayrıca pinlenmeli.* Tek RED de aynı aileden: bir test yorumu *"slayta eklenen bir link de
> testi kırar"* diyordu, oysa `assertRefs` href **değer** kümesini karşılaştırıyor, **sayısını değil** —
> metinsiz, izinli hedefli üçüncü bir `<a>` (görünmez ikinci dokunma hedefi) suite'i yeşil bırakıyordu.
> **Sıradaki:** "ŞU AN" → **M5-08** (QR kanalı; Q15: IP zorunlu, GPS tek başına yetmez).

> **M5-06 done — 2026-08-01 (`b3fb2b5`), iki denetçi ONAY.** **15 tur, 11 RED — projenin en uzun görevi**, ve
> neredeyse hepsi **tek sınıftan**: *bir cümle ya da bir SAYI, sistemin vermediği bir şeyi beyan ediyor.* İlk iki RED
> ekranın metnindeydi (§4.6: `ignored` *"Your earlier tap stands."* — debounce verdict/kanaldan bağımsız olduğu için
> öncül `flag`/`reject` olabilir; `reject` başlığı *"Not recorded"* — oysa render edilen Result satırın **kanıtı**).
> **Kalan dokuz RED'in hepsi DÜZELTMENİN kendisinde çıktı:** her koruma bir sonraki turda yenildi (elle kurulmuş
> golden → metin düğümü listesi → öznitelik listesi → eleman listesi → referans listesi), ve her seferinde bloklayan
> şey mekanizma değil **onu anlatan cümlenin fazla söylemesi** oldu. **Bunun üzerine 11. turda "kapanış kuralı"
> konuldu: yeni kanal KAPATILMIYOR, dürüstçe LİMİT olarak sayılıyor** — ve iş üç tur sonra bitti. Bugün 8 kanal
> limit olarak yazılı. **Sonraki görevlerde geçerli ders:** bir mekanizmayı tarif ederken *"tamamen/bitmiş/complete"*
> yazmadan önce onu **yenmeye çalış**; yenemediğini de nasıl denediğinle birlikte yaz. **Sıradaki:** "ŞU AN" → **M5-07**.

> **M5-01 done — 2026-07-31, 5. oturum.** `internal/session` teslim edildi (`a71e1b2`), **iki denetçi
> ONAY** (genel üçüncü göz 3. turda + `tappa-security-auditor` kapanış turunda). **Beş tur sürdü ve iki
> RED gördü — ikisi de AYNI SINIFTAN:** *dosya, sağlamadığı bir güvenlik garantisini yorum olarak beyan
> ediyordu.* (1) `Token` unexported alanda `%v/%+v/%#v/slog` ile ham token basıyordu (`fmt`,
> `CanInterface()==false` olunca `Formatter/Stringer/LogValuer`'ı **atlar**) → `struct{ v *string }`.
> (2) `Cookies` sıfır değeri prod'da **`Secure`'suz** çerez yazıyordu (Go'da yasak olan alanı
> *adlandırmaktır*, `T{}` yazmak değil) → kutup çevrildi, `struct{ insecure bool }`. **Ders M5-02…M5-10
> boyunca geçerli:** bir yorum "hiçbir çağıran X yapamaz" diyorsa X **harici paketten denenmiş** olmalı;
> denenmediyse *sınır* olarak yazılır. **Sıradaki:** "ŞU AN" → **M5-02** (davet + aktivasyon). **🔴 M5
> için BLOKLAYAN devir (N5) HÂLÂ AÇIK:** tap.Decide tenant-farkındalıksız → M5-03/M5-05 Input'u
> TagTenantID/SessionTenantID ile besleyip `sys:tenant-mismatch`'i ateşlemeli (çapraz-tenant deliği).
> N1–N5 + ErrUnknownTag "M4/M5'e devralınan"da. Kritik durum sohbette kalmıyor.

---

## ŞU AN

| | |
|---|---|
| **Kilometre taşı** | **M0 + M1 + M2 + M3 + M4 + M5 + M6 TAMAM** ✅ 🎉🎉 · **[Dashboard](m6-dashboard.md) 12/12 — müdürün gördüğü ürünün tamamı da bitti.** Sekiz bölüm: transactions · review · employees · locations · reports · anomalies · policies · **billing**. **Sıradaki: M7 — portal & signup, ve ürünün en büyük fonksiyonel boşluğu orada: bugün hiç kimse KAYIT OLAMIYOR, `/` 404 veriyor.** · **[Tap akışı](m5-tap-akisi.md) 11/11 — çalışanın gördüğü ürünün tamamı bitti.** Davet → aktivasyon → mini tur → **NFC veya QR** → karar → kayıt → onay ekranı, **gerçek HTTP + gerçek Postgres üzerinde bir GÜN** olarak kanıtlı (`make simulate-day`: 10 çalışan, 31 kayıt, hepsi **karar motorundan**), tap sayfası **3 dk'lık tazelik penceresine** bağlı, ve §5'in yön kuralı sevk edilmiş kodda **doğru**. **Sıradaki: M6 — müdürün gördüğü taraf.** |
| **Sıradaki görev** | 🔴 **M8-05 FAZ B3 — ANDROID RÖLESİ VE FİZİKSEL DOĞRULAMA. M8'in TEK DONANIMA BLOKE fazı, ve encode aracının kalan tek parçası.** **Sunucu tarafı BİTTİ:** B1 ADR 0017 (`a5b3c2a`) · B2a kripto çekirdeği (`c944431`) · B2b yedi komut (`c170129`) · B2c-1 oturum + sürücü (`6bb6cdf`) · B2c-2a veri katmanı (`b5f4390`) · **B2c-2b uç nokta (`f632f09`)**. Röle artık **uçtan uca koşuyor** — yönetici paneli üç rota, `ProtectWriting` + encode bütçesi, `tags` satırı, **üç `audit_log` olayı**, `encoded_at` damgası. **Kalan: çipe dokunan taraf.** 🔴 **B3'ÜN İLK İŞİ md. 16'DIR: HİÇBİR ÇİP ENCODE EDİLMEDİ** — on bir turluk sunucu tarafının **tamamı** belge okuması, sahte çip ve mutasyondur. **Devir listesi 28 MADDE (5 kapandı, 23 açık)** ve bir sonraki oturumun okuyacağı **tek kayıt**; silikonda ilk koşuda **md. 7 (adım 5'in CommMode'u)**, **md. 8 (`ChangeKey` sonrası oturum yaşıyor mu — sahte çip *"evet"* diyor, `internal/sun/changekey.go` **tersini varsayıyor**)** ve **md. 9/12 (Tablo 65 ön koşulu)** gerçek olur. ⚠️ **Ve iki şey B3'ten ÖNCE karara bağlanmalı:** **md. 5** (anahtar 0'ın nerede saklanacağı — **pilot kapısının 7. maddesi ona bağlı**, ve §5.1 **adım 8 sevk edilmedi**) · **md. 11 = Q08** (host kararlaşmadı; **yanlış host'la encode = sahada plaket değişimi**). 🔴 **VE BİR ŞEY KULLANICIDA:** **md. 17** — `resolve_tag_by_uid` üzerinden anahtar okunabilir, **kapatılamaz** (tap tenant bağlamı olmadan gelir), ama **mevcut mekanizma var ve alınmadı**: ayrı çözümleme rolü, bedeli **ikinci bir havuz + canlı kümede yeni bir Secret**. Ölçüldü (B2c-2b): katalogda **altı** `SECURITY DEFINER`, altısı `tappa_resolver` (**BYPASSRLS**) sahipli, altısında da `EXECUTE` `tappa_app`'te — ve **`resolve_admin_by_email` `password_hash` döndürüyor**. ⚠️ `pool.go`'nun `roleRefusal`'ı düz bir rolü **açılışta fark etmez**. *(Aşağıdaki B2c-2b tarifi TARİHSEL — bitti, `f632f09`.)* **M8-05 FAZ B2c-2b — HTTP RÖLESİ VE YETKİLENDİRME. Bir GÖREV, kullanıcı kararı değil, donanım gerekmez.** **B2c-2a bitti** (`b5f4390`): migration **00022**, `tags`'e ilk INSERT, `Rows`/`Wrapper`, **`audit_log`'a iki olay**, **`tenant_id` açık parametre**, ve ✅ **T16'nın SELECT yarısı** (`aes_key_ref` **on sütunun tek okunamayanı**). **B2c-2b'nin işi: uç nokta.** `RequireAdmin` + oran sınırı (**md.10**; `internal/httpx` ikisini de taşıyor — **bağla, icat etme**) · `internal/encode`'u **bir üretim yoluna bağla** (bugün **sıfır** import, **md.18**) · **md.12** UID işgali. 🔴 **ÜÇ ŞART, üçü de devir listesinden ve üçü de ölçülmüş:** **(1)** md. 19'un adlandırdığı **ayrı çözümleme rolü** kararı — `resolve_tag_by_uid`'i tek çağırana bağlayan **tek mekanizma**, bedeli **ikinci bir havuz**; ⚠️ ve `pool.go`'nun `roleRefusal`'ı **yalnız `Privileged()`'da** ateşliyor, yani düz bir `NOSUPERUSER/NOBYPASSRLS` rol **açılışta fark edilmez** · **(2)** **`keyring` `st.mu` ile KORUNMUYOR** — sahibi `s.busy`'yi tutan goroutine; bir *"oturum ne tutuyor"* sağlık ucu `st.mu` alıp halkayı okursa **canlı anahtar üstünde veri yarışı** olur, ve `st.mu`'yu almış olmak **yanlış bir güvenlik hissi** verir · **(3)** **md.15'in *"süreçten çıkanı"* bağlayan genel kapısı YOK**, ve **HTTP uç noktası tam o yüzeyi doğuruyor**. 🔴 **Kartın "KAPATILMAMIŞ SAYIM" listesi 19 MADDE ve bir sonraki oturumun okuyacağı tek kayıt** — başında **md.16: hiçbir çip encode edilmedi**. *(Aşağıdaki B2c-2 tarifi tarihsel — B2c-2 ikiye bölündü, B2c-2a bitti.)* **M8-05 FAZ B2c-2 — HTTP RÖLESİ, YETKİLENDİRME VE KALICILIK.** **B2c-1 bitti** (`6bb6cdf`): `internal/encode` — bellek içi EV2 oturum deposu + dokuz adımlık sürücü, **portlar tüketici tarafında** (`Rows`·`Wrapper`·`Clock`), kapsam **%92,6**, **12 yapıcı turu / 10 bağımsız denetim / 42 bloklayan**. ✅ **§6 md.7 ve md.12'nin dördüncü sonucu KAPANDI**; `GetCardUID` kapısı **adım 4b**'de, yani **ilk geri döndürülemez komuttan önce** (ADR §5.1 tadil edildi). **B2c-2'nin işi B2c-1'in portlarına GERÇEK uygulama vermektir:** uç nokta + **`RequireAdmin`** + oran sınırı (**md.10**, `internal/httpx` ikisini de taşıyor — **yeni kimlik yüzeyi icat etme, bağla**) · `tags` satırı + **`audit_log` olayı** (**md.8**: olay adı `tag.retired` kalıbıyla tutarlı olmalı, **aktör** ve **hangi tenant** kararlaşmadı) · **md.9** → **T16** · **md.12** UID işgali. 🔴 **İKİ BLOKLAYICI ŞART, ikisi de denetimden ve ikisi de ölçüldü:** **(1)** `Rows.InsertUnassigned` bugün `(ctx, uidHex, wrappedKey)` alıyor — **`tenant_id` YOK**, ve `tags.tenant_id` **NOT NULL, DEFAULT'suz** (00004): tenant'ı `SET LOCAL`'e örtük bırakırsan §4.5'in **kuşağı** (sorguda açık filtre) tamamen uygulamanın hafızasına kalır → **açık parametre olmalı**. **(2)** `tags`'te **"encoded" sütunu YOK**; `status` `unassigned` kalmak zorunda, yani `Rows.MarkEncoded`'ın ilan ettiği ihtiyacı **nasıl karşılayacağın bir şema kararıdır**. ⚠️ **Ve bir yarış tuzağı:** `keyring` **`st.mu` ile korunmuyor** — sahibi `s.busy`'yi tutan goroutine'dir; bir *"oturum ne tutuyor"* sağlık ucu `st.mu` alıp halkayı okursa **canlı anahtar malzemesi üstünde veri yarışı** olur ve `st.mu`'yu almış olmak **yanlış bir güvenlik hissi** verir (denetçi `-race` ile üretti; `armed()` test yardımcısı **tam o deseni** kullanıyor). 🔴 **Kartın "KAPATILMAMIŞ SAYIM" listesi 16 maddedir ve bir sonraki oturumun okuyacağı tek kayıttır** — başında **md.16: hiçbir çip encode edilmedi**. *(Aşağıdaki B2c tarifi tarihsel — B2c ikiye bölündü, B2c-1 bitti.)* **M8-05 FAZ B2c — DURUMLU OTURUM VE HTTP RÖLESİ.** **B2a** (`c944431`, kripto çekirdeği) ve **B2b** (`c170129`, yedi komut) bitti; **encode aracının bayt üreten yarısı tamam**, kalan **taşıma ve ömür**. **B2c'nin kabul kriterleri ADR 0017 §6'da:** **md.7** oturum TTL'i · eşzamanlılık · iptal · **`Zero`'nun ÇAĞRILMA garantisi** (B2a mekanizmayı verdi, garantiyi değil) · **`RndA` `crypto/rand` ile, hata kontrollü, ASLA tekrar kullanılmadan** (bedeli ölçüldü: kaydedilmiş bir çift, aynı `RndA` ile **anahtar olmadan** echo kapısını geçer → §5.3 sonda 2 **sahte başarı**) · **md.10** uç noktada **yetkilendirme kapısı** (`internal/httpx` zaten `RequireAdmin` + `ratelimit` taşıyor) · **md.9** `aes_key_ref`'in **sütun düzeyi INSERT kısıtı** → **T16** · **md.12** **UID işgali**, ve 🔴 **dördüncü sonucu B2b ekledi: yalancı bir röle `UID_X` döndürürken çip `UID_Y` ise, satır `UID_X`'le yazılır ama anahtar `UID_Y`'ye gider → "çip var, satır yok", yani "satır önce" kararının engellemek için var olduğu modun ta kendisi.** Çare **B2b'de sevk edildi**: `GetCardUID` (**kimlik doğrulama ister**, MAC'i röle üretemez) → adım 6'dan sonra UID **yeniden okunur**; **yalanı tespit eder, satırı önlemez**. · **md.5** anahtar 0'ın saklanması **hâlâ açık** ve **pilot kapısının yedinci maddesi ona bağlı**. *(Aşağıdaki B2b tarifi tarihsel — B2b bitti, B2 üçe bölündü.)* **M8-05 FAZ B2b — KOMUT KATMANI VE DURUMLU OTURUM.** **B2a bitti** (`c944431`): EV2 kripto çekirdeği saf fonksiyonlar hâlinde, **%95,9 kapsam**, **11/11 bayt-sırası mutasyonu kırmızı**, ve **XOR sorusu tekrarlanmış bir deney** (XOR silinince tam bir test kırmızı, 29 yayımlanmış vektör yeşil). **B2b'nin işi:** `AuthenticateEV2First` → `ChangeKey` → `ChangeFileSettings` **komut dizisi** (ADR 0017 §5.1'in normatif sırası) · **durumlu encode oturumu** — ve **ADR 0017 §6 md.7 onun kabul kriteri**: TTL · eşzamanlılık sınırı · iptal · **`Zero`'nun ÇAĞRILMA garantisi** (B2a mekanizmayı verdi, garantiyi değil) · `RndA` **`crypto/rand` ile, hata kontrollü, ASLA tekrar kullanılmadan** (bedeli ölçüldü: kaydedilmiş bir `(E(K,RndB), E(K,TI‖RndA'‖caps))` çifti, aynı `RndA` ile **anahtar olmadan** echo kapısını geçer → **§5.3 sonda 2 sahte başarı verir**, gizlilik kaybı değil **teşhis yanılgısı**) · uç noktada **yetkilendirme kapısı** (§6 md.10; `internal/httpx` zaten `RequireAdmin` + `ratelimit` taşıyor) · `aes_key_ref`'in **sütun düzeyi INSERT kısıtı** (§6 md.9 → **T16**) · **UID işgali** azaltması (§6 md.12) · `FileAR.Change`/`ReadWrite` kararı (§6 md.13). ⚠️ **Ve iki tasarım kararı YAZILMALI:** `EV2Auth` **beş alanını ihraç ediyor** ve **`authenticated bool` taşımıyor** (paket dışından elle kurulabilir — bugün zararsız çünkü kurmak için zaten oturum anahtarları gerekiyor); ve **`CmdCtr` monotonluğunu hiçbir Go katmanı garanti etmiyor**, tek fren çipin kendisi. 🔴 **Ve plain SDM `CmdData` düzeni B2b'nin M2-08 riskidir:** yayımlanmış `ChangeFileSettings` örneği **şifreli-PICC** yapılandırmasını kuruyor (`SDMMetaRead: 0x2`), Tappa'nın **plain**'i `Eh` ister ve o zaman alan sırası **yalnız NT4H2421Gx §10.7.1 Tablo 69'dan** okunur (*"Mirror position, LSB first"*). *(Aşağıdaki B2 tarifi tarihsel — B2 ikiye bölündü.)* **M8-05 FAZ B2 — ENCODE ARACININ SUNUCU TARAFI.** FAZ B1 tur 1 bitti: **[ADR 0017](../adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md)** (röle mimarisi · anahtarın hayatı · yarım-yazma kurtarması), **9 yapıcı turu / 8 bağımsız denetim, 7'si RED + 8'incisi ONAY, toplam 24 bloklayan**. **Karara bağlananlar:** kullanıcı **kendi Android app'imizi** seçti (§1 dil zinciri onayı **alındı**) · mimari **APDU rölesi** — kripto sunucuda, düz anahtar çıkmaz, **ADR 0003 md.5 tadil edilmedi** · **`K_SDMFileRead` = uygulama anahtarı `0x01`** · **anahtar `0x00` kişiselleştirilir, EN SON adım** (sevkiyat bir şema kararına bloke — ikinci anahtarı saklayacak yer yok, `tags` tek `aes_key_ref` taşıyor) · encode satırını **`tappa_app`** yazar (owner **mimari olarak imkânsız**: uç nokta HTTP, `pool.go` prod'da owner DSN'i reddediyor) → **backlog T16'nın çözümü uygulanamaz hâle geldi ve vadesi değişti**. **Ve ADR 0005 altıdan SEKİZ riske çıktı** (risk 7 röle APDU dökümü · risk 8 anahtar 0 oltalaması), sayı artık `TestADR0005_TheRiskCountMatchesTheTable` ile **bağlı**. 🔴 **PİLOT KAPISI YEDİ MADDE** (altıydı): duvara çıkan hiçbir plakette anahtar 0 fabrika varsayılanında **olamaz** — mekanik karşılığı **yok**, bugün insan kontrolü. **FAZ B2'nin işi:** EV2 durum makinesi (`AuthenticateEV2First` → `ChangeKey` → `ChangeFileSettings`), oturum anahtarı türetimi, `TI`/`CmdCtr` yaşam döngüsü, durumlu encode oturumu (**TTL + eşzamanlılık + her çıkış yolunda `Zero` = ADR §6 md.7, kabul kriteri**) — ve **vektörler VAR**: AN12196 **rev. 1.8** §6.6 T14 · §6.9 T19 · §6.16 T26/27 (**rev. 2.0:** §5.6 T14 · §5.9 T18 · §5.16 T25/26 — revizyonsuz bir *"§6"* rev. 2.0'da **"Special functionalities"**a düşer) **üçü de tam çalışılmış örnek**, iki bağımsız denetçi doğruladı. ⚠️ **İki eksen KAT ile kapanmıyor** (M2-08 sınıfı, brief'e girmeli): plain SDM `CmdData` düzeni yalnız **NT4H2421Gx §10.7.1 T69**'dan okunur · ve `ChangeKey`'in **XOR yarısını ayırt eden hiçbir yayımlanmış vektör yok** (iki örnekte de eski anahtar sıfır). *(Aşağıdaki dört-yol ölçümü tarihsel — karar verildi.)* **2026-08-20'de DÖRT YOL ÖLÇÜLDÜ** (`deploy/README.md` → *Plaket encode* → *Dört yol*): **A** USB okuyucu + Go (**€42–48** + 1 Go modülü) · **B** kendi Android app (**€0**, yeni dil zinciri) · **C** kendi iOS app (**$99/yıl** + macOS) · **D** üçüncü parti (**$29,99/yıl**, 🔴 **ADR 0003 md.5 ihlali**). 🔴 **Ölçüm ekseni değiştirdi: APDU RÖLESİ** — çipe dokunan taraf yalnız bayt taşır, kripto sunucuda koşar, düz anahtar **çıkmaz** (`ISO 14443-4` `FWT` **PICC'i** sınırlar, PCD'yi değil) → **A·B·C üçü de ADR'yi tadil etmeden** sağlıyor. 🔴 **KARARI BELİRLEYEN TEK ÖLÇÜM YAPILMADI:** iOS'un **~20 sn SERT** sınırının altında tam bir röleli tur yetişiyor mu (kaba hesap ~2,6 sn diyor, **silikonda ölçüm yok**). Yetişirse C↔B aynı sınıf; yetişmezse seçim **A↔B**. **Hiçbir çip encode edilmedi.** ⚠️ Elenenler sebebiyle: **WebNFC kalıcı olarak** ham APDU veremiyor (W3C spec) · **ACR122U/ACR1281U-C1** elendi, sebep **WTX** (*"Time extension requests are not supported"*, oysa `AuthenticateEV2First`/`ChangeKey` tam onu ister) · hazır Go kütüphanesi **yok** (`barnettlynn/nfctools` **lisanssız**, `dumacp/smartcard`'da `SDM` **sıfır**) · **M8-06** → **Q13** (saklama süresinin hukuki onayı) ve **T45** (yedek: `secret/tappa-backup-target` yok; hazırlık tam, hedef **Cloudflare R2**) — ⚠️ **T45 pilotun bloklayıcısıdır**, çünkü pilot haftasının paralel kayıtları §4.3 gereği **yeniden üretilemez** · **M8-07** → M8-06'ya bağlı. ⚠️ **Ayrıca açık: Q28** (uyarı teslimat kanalı + log saklama) · **Q02** (parola sıfırlama teslimat kanalı) · **T25** (sosyal çıkarım, ürün kararı) · **T46** (`ALTER DEFAULT PRIVILEGES`, operatör bir kez koşacak) · ve **bu turun açtığı üç madde: T57** (CI'ı ısıracak flake — ürün kusuru **değil**, ama kırdığında *"append-only bozuldu"* diye okunur) · **T58**/**T59** (ikisi de **ürün kararı**, §9 ve oturum iptali). 🔴 **VE BİR ORKESTRATÖR BORCU:** kabul kriteri 3'ün *"ve yazıldı"* yarısı **F7 için hâlâ sağlanmıyor** — FAZ A'nın F7 bulgusunun metni **repoda yok** ve hiçbir ajan uyduramaz; ağaçtaki `F<n>` etiketlerinin **hiçbiri** M8-04 FAZ A'ya bağlanmıyor (hepsi başka görevlerin numaraları). Metin orkestratörün elindeki FAZ A raporunda; **bir sonraki oturum onu yazmalı ya da kriteri dürüstçe eksik bırakmalı**. *(Aşağıdaki eski B3 tarifi tarihsel — kriter artık kapandı.)* **~~M8-04 FAZ B3~~** — kartın sağlanmamış tek kriteriydi: *"ORTA/DÜŞÜK olanlar ya kapandı ya gerekçesiyle kabul edildi **ve YAZILDI**"*. FAZ A (denetim + 53 satır triyaj), **B1** (`6f4df34`, migration `00021`) ve **B2** (`239d427`, uygulama katmanı) **bitti**; B1/B2 *kapatma* kısmını yaptı, **B3 "ve yazıldı" kısmıdır**: kabul metinleri **ağaca**, ve tercihen kabulü **yanlışlayacak** bir mekanik çapaya bağlı — `docs/backlog.md`'de durmak **yetmez**. 🔴 **B3'ün ölçeceği satırlar:** `T38·T39·T40·T48·T51·T52·T53·T54·T55·T56` (doğrudan M8-04) + `T15·T22·T27·T28·T30·T32·T37` ("M8 pilot öncesi"); her biri için **(A) zaten kapandı** (nerede — dosya:satır + kapıyı süren test) / **(B) ucuz kapanır** / **(C) kabul, metni ağaca**. ⚠️ **B2'nin sekiz SAYILMIŞ LİMİTİ ağaçta yazılı ama backlog'da satırı yok** — `SECURITY DEFINER`+`RETURNS TABLE` üzerinden **dolaylı view** · `ALTER DEFAULT PRIVILEGES` (**mekanik kapısı yok**) · `COPY … FROM/TO PROGRAM` · bileşik totolojiler · sözdizimsel eşanlamlılar · W4'ün ters-tırnaklı yazımları · `maxStaticRanges`'in **yalnız HTTP sınırında** uygulanması (`SaveVenue` sınır koymuyor, sütunda kardinalite CHECK'i yok, `signup.go` `SaveVenue`'yu atlıyor) · `containedInAnother`'ın **O(n²)** okuma maliyeti (n=20000 → 558 ms, bugün erişilemez). 🔴 **VE SON DENETİMİN BIRAKTIĞI İKİ İŞ:** (1) bir **tenant politikası** `transactions.note`'a *"network proof of place"* gibi **sahte bir kanıt cümlesi** yazdırabiliyor (ölçüldü) — **§4 ihlali değil** (§5 satır 6–7'yi adıyla tenant'a veriyor, ve satır `matched_sid='tenant:…'`·`policy_layer='tenant'`·`ip_match=false`·`trust=20` taşıyor: **düzyazı yalan söylerken üç sütun doğruyu söylüyor**), ama fiş/rapor `note`'u basarken **katmanı ayırt etmiyor** → savunma derinliği boşluğu, ucuz kapanır (UI'a dokunulursa **skill `tappa-brand`**); (2) `CREATE RULE … DO INSTEAD NOTHING` sınıfının kapısı (*"7 sayaçtan 6'sı kırmızıya döner"*) **doğrulanmadı** — commit'li DDL gerektiriyordu; sayı yanlışsa harita düzeltilmeli (**dört turdur tekrarlayan sınıf**). **Sonra:** kart kapanışı → ledger `done` → **push**. *(🔴 **Burada iki cümle vardı ve 2026-08-20'de SİLİNDİLER, düzeltilmediler:** *"`make audit` T31'e bloke"* ve *"'`make audit` temiz' bugün **imkânsız**"*. **T31 kapandı, `make audit` exit 0** — ve bu hücrenin **sonu zaten öyle diyordu**, yani tek bir hücre kendi içinde çelişiyordu. Bir denetçi bunu bulmak zorunda kaldı, üstelik orkestratör **bir tablo aşağıdaki sağlık satırını aynı turda düzelttikten sonra**: borç bir kopyada kapatılıp **en çok okunan kopyada** bırakılmıştı. **Ders: bir çelişkiyi düzeltirken cümlenin KAÇ kopyası olduğunu say.**)* 🔴 **VE BU GÖREV ON ÜÇ BACKLOG SATIRININ ADRESİ** — T23·T24·T25·T27·T28·T29·T30·T32·T33·T34·T35·T37 **+ T51/T52 (M6-13'ten) + T53/T54 (bu oturumdan)**; denetim onları **yeniden keşfetmesin, kapatsın ya da gerekçesiyle kabul etsin**. ⚠️ **M8-03'ün devrettiği iki sayılmış limit ilk sıraya:** `TestObservability_AlertSignalNames` **alt-dize** kontrolü yapıyor (bir olay adının **sonuna** karakter eklemek testi düşürmüyor) · ve bu oturumda düzeltilen **dört sayının hiçbirini bir kapı korumuyor**. 🔴 **AYRICA M8-03'ÜN AÇTIĞI SORU KAPIDA: Q28** — uyarıların **teslimat kanalı yok** ve log saklama **süre değil boyut**; M8-06 pilot kapısı buna bağlı. 🔴 **BURADAN SONRASI TARİHSELDİR — 2026-08-19'un durumu, ve DÖRT CÜMLESİ ÖLDÜ.** Ölenler adıyla: *"B **donanıma bloke**"* (B1 ve B2 donanımsız koşar, yalnız B3 çipe bağlı) · *"**KULLANICIDA YENİ BİR KARAR VAR: encode aracı** … aracın kendisi **seçilmedi**"* (**seçildi**: kendi Android app'imiz) · *"**donanım gerekiyor** … **yazıcı yok** … önerilen **ACR1252U (~€45)**"* (o yol **elendi**, seçilen yolun donanım maliyeti **€0**) · *"**maliyet ölçülmedi**"* (dört yol **ölçüldü**). Bugünkü durum için **bu hücrenin başına** ve **ledger'a** bak. **Önceki bölüm (M8-05/M2-08, 2026-08-19):** M8-05 FAZ A ölçülürken `internal/sun`'da **sevk edilmiş bir kripto kusuru** bulundu ve **M2-08** olarak düzeltildi — `sv2()` SDM sayacını SV2'ye URL sırasında (verbatim) yazıyordu, oysa AN12196 rev. 1.8 s.10 sayacı SV2'ye **LSB-first**, URL'ye **MSB-first** koyuyor. Encode edilen ilk çipin **ilk 255 tap'inin hepsi** reddedilirdi. **Üretimde tetiklenmedi çünkü hiç plaket yok** (encode aracı hiç yazılmadı). ⚠️ **Kusuru M2-04 "düzeltmesi" ÜRETTİ ve dört ay boyunca bir test onu ADIYLA korudu** — ders `agent-brief.md`'ye yazıldı. **M8-05 A/B'ye bölündü:** A (runbook, donanımsız yarı) **done**, B **donanıma bloke**. 🔴 **KULLANICIDA YENİ BİR KARAR VAR: encode aracı.** Q10'un *"kendimiz encode ederiz"* yarısı karara bağlandı, ama **aracın kendisi seçilmedi** — kendi Go yazıcımız (**yeni PC/SC bağımlılığı, §1 onayın gerekiyor**) ya da üçüncü parti yazıcı + bizim yükleyicimiz (**düz anahtar süreçten çıkar → ADR 0003 md. 5'i ihlal eder, ADR tadili ister**). İki şekil `deploy/README.md` → *Plaket encode* bölümünde; **maliyet ölçülmedi**. Ve **donanım gerekiyor**: NTAG paketi sende var, **yazıcı yok** — hazır iOS uygulamaları NDEF yazar, 424 DNA'nın `ChangeKey`/`ChangeFileSettings`'ini yapmaz (bir istisna iddiası var, **doğrulanamadı**); önerilen **ACR1252U (~€45)**. **Aşağıdaki 13. oturum zemini hâlâ geçerli:** deploy **ilk kez uctan uca yesil** (`32032319245`, 4m2s) — `01-rbac.yaml` **kumeye uygulandi** (cluster-admin, bir kez) ve `db-role` kapisi **dogdugundan beri ilk kez gercekten kostu**; ayni daraltma `secrets`'i **tum secret'lar + 7 fiil**'den **`tappa-secrets` + `get`**'e indirdi, yani **`TAPPA_TAG_KEK` deploy kimliginin patlama yaricapindan CIKTI** (olculdu: `pods/exec` yalniz `tappa-postgres-0`'da, uygulama pod'unda **hayir**). Docker Hub: iki secret + iki **Public** depo (`is_private=false`, anonim GET ile dogrulandi), PAT `pull,push` veriyor. Canli: `sha-ede8cb9de236` = HEAD, `/healthz` `/readyz` **200/200**. 🔴 **KULLANICIDA BEKLEYEN, ve ikisi de OLCULDU:** **(1) yedek yok** (backlog **T45**) — ama **bugun acil DEGIL**: uretim veritabani **bos** (`tenants=0 employees=0 transactions=0 tags=0`, 8974 kB), yani node olse **kayip sifir**. **Risk ILK GERCEK KAYITLA baslar** (pilotla degil — `/signup` herkese acik ve urun canli). Hazirlik tam: `configmap/tappa-backup-scripts` **kumede**, `50-backup.yaml` `--dry-run=server` **temiz**, eksik **tek sey** `secret/tappa-backup-target`; kullanici karariyla hedef **Cloudflare R2**. **Ve M6-13'un devrettikleri:** **T51** (iki pinleme boslugu; gercek cevap **tip duzeyi logger redaksiyonu**, 224+46 cagri yeri) · **T52** (yerel dev DB'de mutasyon kosularinin yazdigi **128 audit satiri**, append-only) · `/admin/departments` **hala yok** (404, ayri boslук) · `maxEmployeeActionBody` ve `adminview.go` nav etiketi **pinsiz**. ✅ **`make audit` artik exit 0** — kullanici Go'yu **1.26.7**'ye yukseltti, **T31 KAPANDI** (`govulncheck exit=0` + `redline exit=0`, orkestrator kendi olctu 2026-08-20). *(Bu satirin eski hali "exit 2, T31'e bloke" diyordu.)* |
| **~~M8-04 devir notu~~** | *(2026-08-16'da FAZ C kapanınca sıradan düştü; M8-02 artık bloke değil. Tarihsel — **ama on iki backlog satırının adresi hâlâ M8-04.**)* **M8-04 — Güvenlik denetimi.** ⚠️ **SIRA ATLANDI VE SEBEBİ ÖLÇÜM: M8-02 ve M8-03 KULLANICIDA BLOKE.** M8-02 `Q08` (domain — `tappa.mt`/`tappa.io` **alınmadı**, EUIPO taraması yok) ve `Q12` (VPS + managed Postgres seçimi, ~€30-50) istiyor; ikisi de **satın alma kararı**, ölçümle çözülmez. M8-03 **M8-02'ye bağlı** (prod log'ları nerede duracak, saklama süresi ne). **M8-04'ün tek bağımlılığı M8-01 ve o bitti** (`4ddd11f`). **Kart (`m8-deploy-pilot.md` → M8-04):** `tappa-security-auditor` **tam repo** üzerinde, **R1–R8 için kanıtlı rapor** · `make audit` temiz · **KRİTİK/YÜKSEK kalmadı**, ORTA/DÜŞÜK ya kapandı ya **gerekçesiyle yazıldı** · manuel doğrulamalar: **gerçek etiketle replay**, çapraz-tenant erişim, oturum çalma, oran sınırı · ve kartın kendi cümlesi: ***"mekanik taramanın KANIT OLMADIĞI unutulmadı — derin denetim ayrı yapıldı."*** 🔴 **BİR KRİTERİ KULLANICIYA BLOKE VE BUNU BAŞTAN BİL:** *"`make audit` temiz"* bugün **imkânsız** — govulncheck 6 stdlib açığı sayıyor, hepsi `go1.26.5 → 1.26.6` ile kapanıyor (**T31, kullanıcının işi**), `redline-check.sh` tek başına exit 0. Kriteri **dürüstçe böl**: taramanın *tappa kaynaklı* yarısı ile *araç zinciri* yarısı ayrı raporlanır. 🔴 **VE BU GÖREV ON İKİ BACKLOG SATIRININ ADRESİ** — denetim onları **yeniden keşfetmesin, kapatsın ya da gerekçesiyle kabul etsin**: **T23** (sevk edilmiş bir baseline kuralının gerekçesi yanlış ve **müşteriye basılıyor**) · **T24** · **T25** (sosyal çıkarım yeteneği, **ürün kararı**) · **T27**/**T37** (faturalama; T37 bu oturumda açıldı: **zone değiştirerek ilk ücretli ayı öteleme**) · **T28** (HSTS **ürün genelinde yok**) · **T29** (`HEAD`; `/healthz`+`/readyz` yarısı **M8-01'de kapandı**, kalan `/t`·`/activate`·panel) · **T30** (`TAPPA_TRUSTED_PROXIES` boşken giriş bütçesi tek adrese çöker) · **T32** (dev compose `log_statement=all` → digest **ve** `password_resets.token_hash`) · **T33** (`redline-check.sh` R5 **ALTER-only migration'ları göremiyor** — §4.5'i geri alan bir migration ağdan geçer, ölçüldü) · **T34** (bağlantı tükenmesi **kilitlenme**, `Cleanup` sırası) · **T35** (*"aynı transaction ⟹ aynı snapshot"* **yanlış** ve **tap karar motorunda** yazılı). ⚠️ **M8-01'in devrettiği ikisi de burada tartılmalı:** `vcs.modified=true` artefakt **üretilebiliyor** ve tek sinyal bir WARN · `Allow` iki ayrı satırda + `OPTIONS` 405 alıyor. **⚠️ ÖLÇÜM ZEMİNİ (2026-08-15, `4ddd11f`):** `make test` **3507 PASS / 0 FAIL / 0 SKIP · 22 paket**, `make check` **exit 0** temiz ağaçta **252 s** (load 2,05→3,58); **çıplak `go test` DB testlerini SESSİZCE atlar (450 SKIP)** — **SKIP sayısını HER ZAMAN yazdır** (bu oturumda **iki denetçi** o tuzağa düştü, biri **mutasyon altında `ok`** aldı çünkü dört DB testi SKIP oldu); **`.templ` sonrası `make fmt gen`**; **her sayıya `uptime` ve `date -u`**. ⚠️ **8080'i bu repoyla ilgisiz bir konteyner tutuyor** (`lse-authz`) ve **`tappa-db` bu oturumda bir kez kendiliğinden durdu** — sondan önce `docker compose ps`. Son migration **00020**, son ADR **0016**. **Canlı iddia olarak sayı/tarih yazma.** |
| **~~M7-04 FAZ A devir notu~~** | *(2026-08-14'te teslim edildi; tarihsel.)* **M7-04 — Admin daveti, şifre sıfırlama, e-posta.** **Bağımlılık:** M7-03 (**done**) · **Q02 (AÇIK)**. **Kırmızı çizgi: §4.7** (token/kod log'a **yazılmıyor**). 🔴 **M7-04'ÜN BİLMESİ GEREKEN ALTI ŞEY:** **(1) 🔴 Q02 AÇIK VE BU GÖREVİN ÖNKOŞULU** — *"e-posta sağlayıcısı AB bölgesinde, GDPR işleme sözleşmesi var"* bir **satın alma kararı**, ölçümle çözülmez. Kart bunu kriter sayıyor. **Sağlayıcı seçilmeden gerçek gönderim yazılamaz** — ama **taşıyıcı arayüzü + kuyruk + hata yolu** yazılabilir; ⚠️ `open-questions.md` → Q02'yi **oku** ve kullanıcıya **ölçülmüş seçenekler** koy (bugün hiçbir yerde e-posta gönderen kod **yok**, doğrula). **(2) 🔴 BU GÖREV M7-02'NİN (a) LİMİTİNİN GERÇEK KAPANIŞINI TAŞIYOR** — *kayıt-öncesi kalabalıklaştırma*: `MaxCandidates` satır ekmek hâlâ bir e-posta penceresini sahipleniyor ve **gerçek çare e-posta doğrulaması**, taşıyıcısı da bu görev. M7-02'nin görev raporundaki beş limitin **en ağırı** bu. **(3) `password_hash` KURALI ARTIK ŞEMADA** (migration 00018, M7-03 A): `^\$2[aby]\$(0[4-9]|1[0-4])\$…{53}$`. Şifre sıfırlama **`adminauth.Hash` çıktısından başka bir şey yazamaz** — ve `adminauth.Cost`'u **14'ün üstüne** çıkarmak artık **migration ister** (tavan bilinçli: dolgu maliyeti `candidates[0]`'dan alıyor). **(4) `admin_users` GRANT'ı sütun düzeyinde** (00017): UPDATE `(full_name, email, password_hash, role, status, last_login_at)`, INSERT `(id, tenant_id, full_name, email, password_hash, role, status)` — **`created_at` ikisinde de KAPALI** ve bu, 00017'nin **tüm argümanının dayanağı** (ilk-gelen sırası). **Açma.** **(5) KALIPLAR HAZIR — ama YARISINI kopyalama** (projede **yedi** kez kusur oldu): davet kodu **hash'i saklanır, kodun kendisi değil** (`employee_invites` kalıbı, M5-02) · `resolve_invite_by_code_hash` **SECURITY DEFINER** ve ADR 0002 §7'nin sekiz özelliği · tek kullanımlık + süreli + iptal edilebilir (`used_at`/`cancelled_at`, migration 00012) · `ProtectWriting` · audit **`RecordTx` ile aynı transaction'da**. **(6) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-14):** `make test` **3223 PASS / 0 FAIL / 0 SKIP · 19 paket**, duvar **234–565 s** (⚠️ **tamamen yüke bağlı** — bu görevde makine `load 155`'e çıktı ve aynı suite 234 s'den 565 s'ye taşındı; `internal/handler` ~256 s **bilinçli ve kabul edilmiş**, **yeniden açma**); **çıplak `go test` DB testlerini SESSİZCE atlar (420 SKIP)** — **SKIP sayısını HER ZAMAN yazdır**; **`.templ` sonrası `make fmt gen`**, yeni CSS sınıfı varsa **`make css`**; **her sayıya `uptime` yaz**. ⚠️ **`make audit` bugün exit 2** ve sebebi govulncheck (**T31, kullanıcının işi**); `redline-check.sh` tek başına exit 0 — **ve artık `rg` bozuksa/taraması patlarsa exit 2 veriyor**, yani *"atlandı"* ile *"temiz"* karışmıyor. Son migration **00018**, son ADR **0014**. **Canlı iddia olarak sayı/tarih yazma.** |
| **~~M7-03 FAZ B devir notu~~** | *(2026-08-14'te teslim edildi; tarihsel. ✅ **KART ÖLÇÜMÜ ÜÇÜNCÜ KEZ İŞE YARADI** — orkestratör brief yazmadan önce iki kart cümlesini çürüttü (*"panelde karşılığı yok"* ve departman ekranının yokluğu), yapıcı üçüncüsünü çürüttü (`structure='multi'`). ⚠️ **VE ORKESTRATÖRÜN SUNDUĞU ŞIKLARIN İKİSİ DE ELENDİ** — Faz A'da *"yalnız biçim"* tutarsız çıktı, yapıcı üçüncüsünü ölçtü. **Ders: brief'teki şıklar hipotezdir, menü değil.**)* **M7-03 FAZ B — departmanlar ve "tag bekleniyor". FAZ A DONE (`81ce4d5`).** **Araç:** skill `tappa-brand` (B tamamen ekran işi; A veri katmanıydı, mercek ayrımı bu). **Kırmızı çizgi satırı §4.5** ama B **yeni tablo/sorgu getirmiyor** — getirirse `tappa-db-migrator`. 🔴 **B'NİN İKİ İŞİ, VE ORKESTRATÖRÜN ÖLÇÜMÜ İKİSİNİ DE DARALTTI:** **(1) DEPARTMANLAR — panel ekranı ZATEN VAR.** `web/templates/pages/locations.templ` departmanlardan **34 kez** söz ediyor ve sekiz sorgu sevk edilmiş (`ListPanelDepartments`, `CreateDepartment`, `UpdateDepartment`, `DeleteDepartment`, `CountDepartmentReferences` …). ⚠️ **Ve `employees.department_id` NULLABLE** (`00003:40`, yorumu *"departman isteğe bağlı bir organizasyon birimidir"*) — yani departmansız bir tenant §5'te **doğru çalışıyor**, vardiya lokasyondan çözülüyor. **Orkestratörün ön kararı: sihirbaza DÖRDÜNCÜ ADIM EKLEME** — en yüksek terk oranının olduğu yerde, opsiyonel bir örgüt birimi için, panelde zaten iyi çalışan bir akışın kopyası. **Yapıcı bunu YANLIŞLAMAYA çalışmalı**; yanlışlarsa ölçüm kazanır. Geriye kalan gerçek soru: `structure='multi'` seçen bir tenant departmanların **var olduğunu** nasıl öğreniyor? **(2) "TAG BEKLENİYOR" — KARTIN *"panelde karşılığı yok"* CÜMLESİ YANLIŞ.** `locations.templ:246` zaten `EmptyState("No plaques are mounted")` + *"No plaques have been loaded for this business yet. Tappa encodes each plaque and loads it here"* basıyor ve **buton koymamayı** gerekçesiyle yazıyor (panel plaket **yaratamaz** — anahtarı tutmak demek olurdu, kullanıcı kararı 2026-08-08). **Gerçek boşluk: İLK İNİLEN EKRAN.** Yeni tenant `/admin`'e (Transactions) düşüyor ve orada hiçbir şey ona plaketinin yolda olduğunu söylemiyor. **Ölç ve şeklini seç.** 🔴 **M7-02'DEN DEVRALINAN AÇIK SINIRLAR — (b) ARTIK KAPALI** (Faz A, migration 00018), kalan dördü: **(a)** kayıt-öncesi kalabalıklaştırma → **M7-04** (e-posta doğrulaması, taşıyıcı yok, Q02) · **(c)** VAT sütunlarında UPDATE yetkisi yok → **M7-05** · **(d)** `httpx.Limiter` süreç içi ve adres başına · **(e)** `signInBlocked` sayılan bir kanal. ⚠️ **VE FAZ A'NIN KENDİ İKİ LİMİTİ:** 210× cost-4 kolu **disiplinsel** kaldı (kapatmanın bedeli **+156 s/paket**, ölçüldü) · `NOT VALID` kalıntısı **donmuş**, `VALIDATE`'i hiçbir şey koşturmuyor. **(3) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-14):** `make test` **3184 PASS / 0 FAIL / 0 SKIP · 19 paket**, duvar **270–390 s** yüke göre (⚠️ `internal/handler` ~256 s **bilinçli ve kabul edilmiş**, **yeniden açma**); **çıplak `go test` DB testlerini SESSİZCE atlar (415 SKIP)** — **SKIP sayısını HER ZAMAN yazdır**; **`.templ` sonrası `make fmt gen`**, yeni CSS sınıfı varsa **`make css`**; **her sayıya `uptime` yaz** — bu görevde **cost 12 için dört farklı okuma** çıktı (210/214/288/312 ms) ve **tek sebebi yüktü**. ⚠️ **`make audit` bugün exit 3** ve sebebi govulncheck (**T31**, kullanıcının işi), `redline-check.sh` tek başına exit 0. Son migration **00018**, son ADR **0014**. **Canlı iddia olarak sayı/tarih yazma.** |
| **~~M7-03 kart ölçümü (Faz A öncesi)~~** | *(2026-08-14'te teslim edildi; tarihsel — **kart ölçümü yine işe yaradı**: üç kriterin sevk edilmiş olduğunu devir notu söylediği için yapıcı hiçbirini tekrar etmedi, ve *"tag bekleniyor panelde yok"* cümlesinin yanlış olduğunu orkestratör **brief yazmadan önce** buldu.)* ✅ **Zaten karşılananlar** (gerekçe ve ölçüm M7-03 kartının 2026-08-13 düzeltme bloğunda): **tek transaction** (`signup.Provisioner.Provision` — tenant + lokasyonlar + ilk admin + `audit_log`, hepsi bir `db.WithTenant` içinde, `RecordTx`) · **RLS izolasyonu** (`TestSignupProvision_IsInvisibleToEveryOtherTenant`, **filtresiz** sondalar + pozitif kontrol) · **rollback** (üç test; ⚠️ **üçüncüsü 2. turda eklendi çünkü ilk ikisi YANLIŞ VAKAYI kanıtlıyordu** — ikisi de **ilk ifadede** patlıyor, yani geri alınacak bir şey hiç yazılmıyor). ⬜ **GERİYE ÜÇ ŞEY KALDI:** **(1) DEPARTMANLAR** — sihirbaz departman **sormuyor**; `structure='multi'` onları açıyor ve panel ekranı **M6-06'da sevk edildi**, yani soru *"sihirbaza mı eklenecek, panelde mi bırakılacak"* — **ölç ve karar ver**. **(2) "TAG BEKLENİYOR" DURUMU** — yeni tenant `tags` satırı olmadan doğuyor ve **doğrusu da bu** (plaketi Tappa encode edip gönderiyor, **kullanıcı kararı 2026-08-08**), ama bugün müşteriye bunu **söyleyen bir durum yok**: done ekranı yalnız düzyazıyla *"plaket gelene kadar dokunulacak bir şey yok"* diyor, **panelde karşılığı yok**. **(3) M7-02'nin LİMİT olarak devrettikleri** (aşağıda). 🔴 **M7-02'DEN DEVRALINAN BEŞ AÇIK SINIR:** **(a) kayıt-öncesi kalabalıklaştırma** — `MaxCandidates` satır ekmek hâlâ pencereyi sahipleniyor; **gerçek kapanış e-posta doğrulaması** ve **taşıyıcı YOK** (Q02 · **M7-04**) · **(b) `password_hash` format CHECK'i YOK** — `adminauth` bunu **~1,9M× zamanlama sızıntısı** olarak adlandırıyor; ⚠️ engel **ürün verisi değil**: seed'in **140/140** satırı bcrypt biçimli, gerçek engel **beş test dosyasının placeholder yazması** + **T26/T22** · **(c) VAT sütunlarında UPDATE yetkisi yok** → zaman aşımına uğrayan kontrol **yeniden koşturulamıyor** (**M7-05**) · **(d)** `httpx.Limiter` **süreç içi ve adres başına** · **(e)** `signInBlocked` sayılan bir kanal. **(4) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-13):** `make test` **3152 PASS / 0 FAIL / 0 SKIP · 19 paket**, duvar **~286 s** (⚠️ `internal/handler` ~256 s **bilinçli ve kabul edilmiş** — yukarıdaki ölçülmüş sınıra bak, **yeniden açma**); **çıplak `go test` DB testlerini SESSİZCE atlar (411 SKIP)** — **SKIP sayısını HER ZAMAN yazdır**; **`.templ` sonrası `make fmt gen`**, yeni CSS sınıfı varsa **`make css`**; **ölçüm alırken `uptime` yaz** (bu görevde çekişme iki ölçümü bozdu). Son migration **00017**, son ADR **0013**. **Canlı iddia olarak sayı/tarih yazma.** |
| **~~M7-02 devir notu~~** | *(2026-08-13'te teslim edildi; tarihsel. ✅ **ÖLÇÜMLERİMİN EN DEĞERLİSİ `00011`'İN 4. YÜKÜMLÜLÜĞÜNÜ BULMAKTI** — kart onu bilmiyordu ve görevin merkezi o oldu. 🔴 **AMA BİR ÇERÇEVEM VE BİR VARSAYIMIM ÇÜRÜTÜLDÜ:** dolgu ticaretini satın alan cümleyi **ölçmeden** kabul ettim, ve *"yeniden-hash artık gereksiz"* dedim — yapıcı ikisinde de **durup ölçtü**.)* **M7-02 — Kayıt sihirbazı ve VAT. ÜRÜNÜN EN BÜYÜK FONKSİYONEL BOŞLUĞUNU KAPATAN GÖREV: bugün hâlâ hiç kimse kayıt olamıyor.** **Bağımlılık:** M7-01 (**done**) · M1-02 (**done**) · **Q09 (AÇIK)**. **Araç:** skill `tappa-brand`. **Kırmızı çizgi: §4.5** (yeni tenant **yaratıyor**) + **§4.1'e değiyor** (*"gerçek işletme filtresi"* VAT'tır, biyometri değil). 🔴 **M7-02'NİN BİLMESİ GEREKEN YEDİ ŞEY:** **(1) 🔴 Q09 AÇIK VE ÖNERİSİ VAR:** *"VIES doğrulaması MVP'de zorunlu mu? Servis sık kesilir. **Öneri:** format zorunlu + VIES **'en iyi çaba'**, başarısızsa `vat_verified=false` ve panelde uyarı."* ⚠️ **Ve bu öneri bir SÜTUN ima ediyor: `tenants`'ta `vat_verified` YOK** (ölçüldü — sütunlar `id, name, vat_number, business_type, structure, plan, timezone, created_at, price_per_employee_month`) → **migration 00017**, §6 beşlisiyle. Kart *"servis kesintisi kayıt akışını DURDURMUYOR"* diyor — yani VIES **senkron bir kapı olamaz**. **Ölç ve karar ver.** **(2) ✅ M6-12 A'NIN DARALTTIĞI GRANT SİHİRBAZIN İHTİYACIYLA BİREBİR ÖRTÜŞÜYOR — bu bir tesadüf değil, o görev bunu öngördü.** `tappa_app` `tenants`'a **yalnız** `(id, name, vat_number, business_type, structure, timezone)` INSERT edebiliyor; `plan`, `created_at` ve `price_per_employee_month` **kapalı** ve DEFAULT'larla doluyor (`founding` / `now()` / `1.50`). ⚠️ **`id`'yi sihirbaz YAZMAK ZORUNDA** — `tenants` politikası `id` üzerinde kapsamlanıyor, yani `DEFAULT gen_random_uuid()` RLS bağlamıyla **asla eşleşmez** ve her INSERT `WITH CHECK`'e takılır. **(3) 🔴 BOT KORUMASI: *"rate limit + basit challenge, CAPTCHA üçüncü parti DEĞİL"*** — §1 gereği (Node yok, dış çağrı yok). ⚠️ **Ve M7-01 BİLİNÇLİ OLARAK ORAN SINIRSIZ** (gerekçe `marketing.go`'da: handler'ın kıt kaynağa ulaşan **alanı yok**); **M7-02'nin POST'u BUNU MİRAS ALAMAZ** — tenant yaratıyor, kendi bütçesini ve challenge'ını **kurmak zorunda**. **(4)** `structure` (`single|multi`) lokasyon/departman modelini belirliyor; **sunucu doğrulaması tam**, istemci yalnız kolaylık. **(5)** Sihirbaz bitince `signupHref` **dolacak** — bugün `""` ve landing'in CTA'sı ona göre çiziliyor (`marketing.go:91-94`); M7-01'in testi ölü butonu **yakalıyor**. **(6) M7-01'İN KALIPLARI GEÇERLİ — ama YARISINI kopyalama** (projede **yedi** kez kusur oldu): kimliksiz yüzeyin savunması **struct'ın şekli** · `Cache-Control` **panelden kopyalanmaz** (⚠️ ve **sihirbaz ÖNBELLEĞE ALINAMAZ** — form durumu taşıyor, `no-store` gerekir) · her iddia **çapaya** bağlı · muafiyet **görünmez olamaz** (ADR 0012). **(7) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-13):** `make test` **2960 PASS / 0 FAIL / 0 SKIP · 18 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar** (**403 SKIP**) — **SKIP sayısını HER ZAMAN yazdır**; **`.templ` sonrası `make fmt gen`**, yeni CSS sınıfı varsa **`make css`** (`make check` onu koşmuyor). Son migration **00016**, son ADR **0012**. **Canlı iddia olarak sayı/tarih yazma** — bu oturumda **beş** vaka çıktı. |
| **~~M7-01 devir notu~~** | *(2026-08-13'te teslim edildi; tarihsel. ✅ **ÜÇ ÖLÇÜMÜM DE İŞE YARADI** — `/`'ın 404 olduğu, fontların **zaten** self-host olduğu (kart bilmiyordu) ve yasal metinlerin **uydurulmaması** gerektiği. ⚠️ Ama **kartın istediği bir tablonun §4 tarayıcısını kıracağını** ölçmemiştim; onu yapıcı buldu ve **ADR 0012** gerekti.)* **M7-01 — Landing sayfası. M7'NİN İLK GÖREVİ, VE ÜRÜNÜN EN BÜYÜK FONKSİYONEL BOŞLUĞUNUN KAPISI: bugün HİÇ KİMSE KAYIT OLAMIYOR.** **Bağımlılık:** M6-02 (bileşenler), **done**. **Araç:** skill `tappa-brand`. **Kırmızı çizgi satırı yok** ama §4.1'e **doğrudan** değiyor — sayfanın **satış argümanı** biyometri yasağı, yani metin ürünün gerçekten yapmadığı bir şeyi **doğru** anlatmalı. 🔴 **M7-01'İN BİLMESİ GEREKEN ALTI ŞEY:** **(1) 🔴 `/` BUGÜN 404 — ölçüldü.** `internal/httpx/router.go` kök seviyede yalnız **iki** şey kaydediyor: `/healthz` (satır 59) ve `/static/*` (satır 64). Yani landing sayfası **yeni bir kök rota** demek, ve ⚠️ o rota **kimliksiz/herkese açık** olacak — panelin bütün korumaları (`Protect`, `ProtectWriting`, oturum bütçesi) **buraya uymaz**; hangi middleware'in geçerli olduğunu **ölç** (rate limit? real-ip? CSP?). **(2) ✅ FONTLAR ZATEN SELF-HOST — kart bunu bilmiyor.** `web/static/fonts/` **M5-04'ten beri** dolu (Space Grotesk + IBM Plex Mono, SIL OFL, woff2, latin + latin-ext) ve `input.css:26+` `@font-face`'leri **kendi origin'imize** işaret ediyor. Kartın *"fontlar self-host, harici çağrı yok (GDPR)"* kriteri **bugün karşılanıyor**; senin işin onu **bozmamak** ve **kanıtlamak** (dış çağrı taraması). **(3) 🔴 YASAL SAYFALAR (Q23) BİR İÇERİK KARARI, KOD DEĞİL** — gizlilik politikası · hizmet şartları · imprint/şirket künyesi · çerez bilgilendirmesi. Kart *"denetimde çıktı: planda hiçbir yasal metin yoktu"* diyor. ⚠️ **Bunları UYDURMA.** Malta'da kurulu gerçek bir şirketin künyesi, gerçek bir veri sorumlusu ve gerçek saklama süreleri gerekiyor — **iskeleti kur, metni kullanıcıdan iste**, ve eksik olanı **ekranda görünür** bırak (bu projenin kuralı: sağlamadığın garantiyi beyan etme). **(4) FİYAT ARTIK ŞEMADA VAR:** `tenants.price_per_employee_month numeric(10,2) DEFAULT 1.50` (M6-12 A, migration 00016) ve `plan` CHECK'i `founding|standard`. Landing **kamuya açık teklifi** yazar (€1.50 + 3 ay ücretsiz) — bu bir **pazarlama metni**, tenant kaydı değil; ama iki sayı **çelişmemeli**. **(5) ANİMASYON `prefers-reduced-motion` SAYGILI, sallanan/gradient efekt YOK** (kart + §9). Hero *"canlı plaket + basılan adisyon"* istiyor — **kitchen-docket motifi zaten var** (`web/templates/components/`), yeniden icat etme. **(6) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-13):** `make test` **2932 PASS / 0 FAIL / 0 SKIP · 18 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar** (bu oturumda **orkestratörü** 396 SKIP ile yanılttı) — **SKIP sayısını HER ZAMAN yazdır**; **`.templ` sonrası `make gen` YETMEZ — `make fmt gen`**; **yeni CSS sınıfı eklersen `make css`** (⚠️ `make check` onu **koşmuyor**, M6-12 B bu tuzağı bir testle kapattı). Son migration **00016**. **Canlı iddia olarak sayı/tarih yazma** — bu oturumda **dört** vaka çıktı, biri orkestratörün. |
| **~~M6-12 FAZ B devir notu~~** | *(2026-08-13'te teslim edildi; tarihsel. 🔴 **BİR ÇERÇEVEM ÇÜRÜTÜLDÜ:** brief rol kapısı için *"(a) motora bağla — M6-09 B'nin yolu"* diyordu; yapıcı üç ölçümle (a)'nın **burada yanlış** olduğunu gösterdi ve denetçi dördünü de doğruladı. **Ders: bir emsal ancak MEKANİZMASI da taşındığında emsaldir.**)* **M6-12 FAZ B — fatura taslağının YÜZÜ. M6'NIN SON İŞİ.** Faz A **done** (`e085ae6`, migration **00016**). **Araç:** skill `tappa-brand`. 🔴 **FAZ A'NIN DEVRETTİĞİ ON BİR YÜKÜMLÜLÜK:** **(1)** `unstamped_employees > 0` ⇒ ekranda **ve** CSV'de cümle — sayı sıfırdan büyükse `EmployeeCount` bir **tabandır** (`Report.Truncated`'ın taşıdığı yükümlülüğün aynısı; A sayıyı üretti, **söylemedi**) · **(2)** `Draft.Frozen` **ayırt edilmeli** — dondurulmuş rakam ile canlı önizleme aynı görünürse görevin tamamı boşa gider · **(3)** founding uyarısı: veri hazır (`FirstChargeableMonth`), koşul = *içinde bulunulan ay ≥ FirstChargeableMonth*; ⚠️ **kayıt tarihi OYNAK** — `seed.sql` `now() - interval '90 days'` yazıyor, **tarihi olgu diye yazma** · **(4)** "dönemi kapat" POST'u: `ProtectWriting` + onay + rol kapısı (owner); `Book.Close` hazır, **rota/CSRF/oran sınırı yok** · **(5)** **`audit_log` yazımı ZATEN YAPILDI, B TEKRAR YAZMASIN** — `Book.Close` aynı transaction'da `billing.period_closed` yazıyor (para **metin**; jsonb sayısı her okuyucuda float64'tür) · **(6)** tavan: `HistoryCap = 60`, `History` ikinci dönüş değeri `truncated` → ekran *"daha eski dönemler"* demeli · **(7)** CSV: `Money.String()` sembolsüz/gruplamasız; formül kaçışı **M6-07 B'nin kalıbı** (Unicode'a bağlı, elle liste değil) + BOM kararı aynen · **(8) 🔴 PDF BLOKELİ** — kart *"CSV/PDF"* diyor, repoda PDF üreteci yok ve §1 Node'u yasaklıyor ⇒ **yeni Go bağımlılığı**, §1 gereği **önce sor** · **(9)** para sembolü **render katmanının**; `DefaultCurrency = "EUR"` sütun DEFAULT'una testle bağlı · **(10)** boş ay seçimi: `Month{}` "hangi ay" demiyor, B **son biten ayı** tenant zone'unda seçmeli · **(11) 🔴 EKRAN BİR İTEMİZASYON TEKLİF EDEMEZ** — dondurulmuş satır **sayıyı** donduruyor, **kimlerin sayıldığını** değil; anonimleştirilmiş kişiler çözülmez, ve id saklamak append-only satırda **GDPR silmesinin erişemeyeceği kalıcı bir kopya** olurdu (gerekçe `00016`'da yazılı). 🔴 **VE FAZ A'NIN DERSLERİ B'DE DE GEÇERLİ:** okuma yolu **hiçbir şey yazmamalı** ve bunu **ölçerek** kanıtla · ekran metni **sağlamadığı garantiyi beyan etmesin** · **canlı iddia olarak sayı yazma, ÜRETEN KOMUTU yaz** (bu görevde **üç** oynak iddia çıktı, biri denetçinin görmediği) · ve **bir kusuru düzeltirken KARDEŞLERİNİ ara** (projede **dördüncü** kez). **⚠️ ÖLÇÜM ZEMİNİ (2026-08-13):** `make test` **2865 PASS / 0 FAIL / 0 SKIP · 18 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar** (bu oturumda **orkestratörü** yanılttı: 396 SKIP); `internal/domain/billing` DB testleri **`DATABASE_MIGRATE_URL` de** istiyor; **`.templ` sonrası `make gen` YETMEZ — `make fmt gen`**. Son migration **00016**. |
| **~~M6-12 FAZ A devir notu~~** | *(2026-08-13'te teslim edildi; tarihsel. 🔴 **BİR CÜMLESİ ÖLÇÜLMEMİŞTİ VE ÜÇ YERE YAYILDI:** Ö5 *"kirlilik seed'de **ve** test fixture'larında"* diyordu — **seed sıfır** üretiyor. Yapıcı bunu daraltıp migration yorumuna, testine ve kart bloğuna yazdı; 1. tur denetçisi çürüttü. ✅ **Ama Ö4 (`deactivated` terminaldir) ve Ö5'in RLS tuzağı uyarısı işe yaradı** — ikisi de doğrulandı.)* **M6-12 — Çalışan sayımı ve fatura taslağı.** **Bağımlılık:** M6-05 · M6-07, **ikisi de done**. **Kırmızı çizgi satırı YOK** ama §4.6'ya değiyor (dondurulmuş sayım = kayıt) ve **§6'ya sertçe**: *"para/saat hesabı `float` ile yapılmaz"*. **Q24 kararı:** iki müşteri için **otomatik ödeme MVP dışı**, ama **sayım otomatik olmalı** — çalışan sayısı ay içinde değişince faturalanacak rakam tartışmalı hâle gelmesin. **Araç:** skill `tappa-brand`. 🔴 **M6-12'NİN BİLMESİ GEREKEN ALTI ŞEY:** **(1) 🔴 KABUL KRİTERİ ŞUNU İSTİYOR: *"sayım geçmişi saklanıyor; geçmiş bir ayın rakamı sonradan YENİDEN HESAPLANMIYOR, DONDURULMUŞ değer okunuyor (çalışan sonradan silinse bile fatura değişmez)"*.** Bu bir **tablo** demek → **migration** (§6 beşlisi: `tenant_id NOT NULL` · `ENABLE`+`FORCE ROW LEVEL SECURITY` · politika · indeks · **GRANT**). Ve dondurulmuş bir fatura satırı **§4.3 ailesindendir** — `REVOKE UPDATE, DELETE` + `tappa_forbid_mutation()` kalıbı (`policy_versions`/`transactions`/`audit_log` üçü de böyle). **Ölç ve bana bildir**; son migration **00015**. **(2) FİYAT TENANT KAYDINDAN OKUNUYOR, KODA GÖMÜLÜ DEĞİL** — kartın açık kriteri. `tenants`'ta böyle bir sütun **var mı, ölç**; yoksa aynı migration'ın işi. **(3) "FATURALANABİLİR ÇALIŞAN" TANIMI EKRANDA YAZILI OLMALI** (ör. *"ayın herhangi bir gününde `active` olan"*) — ve ⚠️ **M6-05'in dersi burada geçerli:** ayrılmış personel `employees`'te **kalıyor** (§4.6), yani tanım **durum + zaman** ister. **(4) FOUNDING OFFER: ilk 3 ay ücretsiz**; 3. ayın sonunda panelde **ve** raporda **görünür uyarı** — *"aksi hâlde ücretsiz dönem sessizce uzar"*. **(5) M6-07/M6-11'İN KALIPLARI GEÇERLİ — ama YARISINI kopyalama** (bu projede **altı kez** kusur oldu): CSV export ise **M6-07 B'nin kaçış sınırı** (Unicode'a bağlı, elle liste değil) ve **BOM kararı** aynen geçerli · okuma yolu **hiçbir şey yazmamalı** ve bunu **ölçerek** kanıtla · tavanlar **ekranda ilan** + `readLimit()`/`truncatedBy()` (artık **paylaşılan**, `report.go`) · audit **`RecordTx` ile aynı transaction'da** eğer yazma varsa. **(6) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-12):** `make test` **2739 PASS / 0 FAIL / 0 SKIP**, **17 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar** (bu görevde **üç** denetçiyi yanılttı); **`.templ` sonrası `make gen` YETMEZ — `make fmt gen`**. Son migration **00015**. **Canlı iddia olarak sayı yazma** — M6-11'de bu kusur **üç kez** çıktı ve her seferinde sayıyı **yapıcının kendi fixture'ı** üretmişti. |
| **~~M6-11 devir notu~~** | *(2026-08-12'de teslim edildi; tarihsel. ✅ **KART ÖLÇÜMÜM İŞE YARADI:** devir notuna *"kartın BİLMEDİĞİ ikinci bir kaynaksız kriter var"* diye yazdığım madde (`device_info` **25 farklı değer / 7.430 oturum**) yapıcının ilk turda doğru kararı vermesini sağladı. **Ders: kartı ölçmek, brief'in en yüksek getirili adımı.** ⚠️ Ama **üçüncü** kaynaksız yarıyı ben de kaçırdım — *"hiç çapraz-lokasyon göstermeyen çalışan"* (**974/974**) yapıcının kendi ölçümüydü.)* **M6-11 — Anomali ve kötüye kullanım raporu.** **Bağımlılık:** M6-07 · M5-10, **ikisi de done**. **Kırmızı çizgi satırı YOK** ama §4.2 ve §4.7'ye değiyor (ekran GPS sinyallerini gösterecek — **mesafe/oran göster, koordinat GÖSTERME**; M6-07'nin `ledger` tip grafı dökümü kalıbı hazır). **Araç:** skill `tappa-brand`. 🔴 **M6-11'İN BİLMESİ GEREKEN BEŞ ŞEY:** **(1) 🔴 KABUL KRİTERLERİNDEN BİRİNİN BUGÜN KAYNAĞI YOK ve kart bunu ADIYLA yazıyor** — *"POST'suz `GET /t` sayısı"* `tap_page_views` tablosunu varsayıyordu, o tablo **kullanıcı kararıyla yapılmadı**; `GET /t` bugün **stateless**. **Ya kendi kaynağını üret** (sayaç/tablo + saklama + RLS beşlisi; kimliksiz `GET /t` **303'te durduğu** için yazma yolu **oturumlu** isteklerle sınırlı) **ya da sinyali DÜŞÜR** — kartın kendi cümlesi düşürmeyi kolaylaştırıyor (uçak modunda zaten sıfır kalır) ve A1'in **asıl** izi olan `ctr` boşlukları **M5-05'ten beri canlı**. **Karar senin, ölçerek ver.** **(2)** Sinyaller **motorun ürettiği bağlamdan** okunacak: `tap:ctrGap` · `tap:gpsConflict` (Y-E: *"IP eşleşti ama GPS uyuşmuyor"* — **GPS-only oranında görünmez**, kayıtları tersine en güvenilir gösterir) · Y-D (tek cihazdan aktive edilmiş çoklu çalışan, hiç çapraz-lokasyon göstermeyen çalışan) · eş-zamanlı tap çiftleri. **`policy_context jsonb` 1. günden beri dolu** (M3-07). **(3) BU BİR TESPİT EKRANI, SUÇLAMA DEĞİL** — kart *"bakılacak yer"* diyor; ekran metni bunu **söylemeli** (§9: çalışan tap ekranı kutsaldır, ama müdür ekranı da bir insanı damgalamamalı). **(4) M6-09'UN KALIPLARI GEÇERLİ — ama YARISINI kopyalama** (bu projede **beş kez** kusur oldu): okuma yolu **hiçbir şey yazmamalı** ve bunu **ölçerek** kanıtla (M6-09 A'nın statik çağrı grafı + canlı sayım kalıbı); ekran metni **sağlamadığı garantiyi beyan etmesin** (M6-09'da **on beşten fazla** vaka); elle tutulan sayı **bayatlar** → türet ya da komutu yaz. **(5) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-12):** `make test` **2710 PASS / 0 FAIL / 0 SKIP**, **17 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar** (bu oturumda **iki denetçiyi** yanılttı); **`.templ` düzenledikten sonra `make gen` YETMEZ — `make fmt gen`** (sekiz tur boyunca `make check`'in yeşil sanılmasının sebebi buydu). Son migration **00015**. **Canlı iddia olarak sayı yazma.** |
| **~~M6-09 FAZ B devir notu~~** | *(2026-08-12'de teslim edildi; tarihsel. ⚠️ **BİR TALİMATIM KUSUR ÜRETTİ VE DENETÇİ ÖLÇTÜ:** 5. turda *"aynı adı ikinci kez yazmayı reddet"* dedirtmiştim; yapıcı bunu `SELECT EXISTS` ile yaptı, **oku-sonra-yaz** oldu ve **eşzamanlı bağlantı başına bir geri alınamaz ikiz** üretti → **migration 00015** gerekti. **Ders: bir kayıp-önleme talimatı, kilidin NEREDE olacağını söylemezse yeni bir yarış açar.**)* **M6-09 FAZ B — Policy yönetim ekranının YAZMA tarafı.** Faz A **done** (`6738687`). **Kırmızı çizgi: §4** (guardrail'ler görünür ama kapatılamaz — A bunu **üç duvarla yapısal** kıldı, B onları **kırmamalı**). **Araç:** skill `tappa-brand`. 🔴 **B'NİN MİRAS ALDIĞI ON YÜKÜMLÜLÜK** (kartın 2026-08-11/12 blokları): **(1)** baseline **aç/kapa** (`policies.enabled` UPDATE'i — GRANT'ta **bilinçli** açık) · **(2)** tenant politikası oluştur/düzenle = **YENİ SÜRÜM** (⚠️ `policy_versions` **DB düzeyinde append-only**: `REVOKE UPDATE, DELETE` + `tappa_forbid_mutation()` — yani bu kriter **şema tarafından zorlanıyor**, disipline bağlı değil) · **(3)** `resource` bağlama CRUD · **(4)** **her değişiklik `audit_log`'a** · **(5)** aralık doğrulaması **arayüzde de** · **(6) `policy:edit`'in GERÇEKTEN zorlanması** — ⚠️ **üç görev üst üste erteledi** (M6-07 B, M6-08, M6-09 A) ve gerekçe her seferinde ölçüldü: panelde `Evaluate`'i **kapı olarak** çağıran handler yok, baseline **iki role de** veriyor, **ve `checkin.forTenant` baseline'ı MATERIALISE ediyor** (bir panel POST'u policy tablolarına **yazardı**) · **(7)** eski sürüm **gövdesi** için sınırlı ayrı rota · **(8)** GPS yarıçapı (25–1000 m) sınırlı parametresi · **(9)** *"hemen yürürlüğe girer"* endişesine **simülasyonsuz** cevap (⚠️ kartın tuzağı **M6-10'a** atıf veriyordu, o `skipped` — Q22 → M9-06; **kart düzeltildi**, üç alternatif ve maliyetleri yazılı) · **(10)** `ListPolicySet`'in **LIMIT'i yok** (tap yolundan miras; gerekçe: *"LIMIT karar setini budar ve kısmi baseline FARKLI bir politikadır"*). 🔴 **VE A'NIN ÜÇ DUVARINI BOZMA:** domain tipi kontrol alanı **taşımıyor** · view tipi **yalnız string** · ve bir test şablonun **her dalını** render edip inert olmayan her eleman/özniteliği kırmızıya çeviriyor. B bir **form** ekleyecek — o form **bölümün içinde** olacak ve ağ onu **görecek**; guardrail bölümüne **hiçbir kontrol giremez**. ⚠️ Ağın **sayılmış kör noktaları** test dosyasının *"NE TUTAR / NE TUTMAZ"* envanterinde: `switch`/`case` ve düz Go + `templ.Raw` **görünmez** (**18 kol / 9 şablon**, türetiliyor) · **ödünç tanık** geçer · kuyruk-öneki · kabuğa konan kontrol. **(11) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-12):** `make test` **2657 PASS / 0 FAIL / 0 SKIP**, **17 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar**; `make check` `gen` de koşuyor. Son migration **00014**. Türetilmiş §4.5 kuşağının kör noktası: **`INSERT … VALUES` görünmez** (ürün genelinde **7 sorgu**) ve **INSERT'ün yazdığı `tenant_id`'ye hiç bakmıyor** — backlog **T20**. **Canlı iddia olarak sayı yazma.** |
| **~~M6-09 devir notu~~** | *(2026-08-11'de teslim edildi; tarihsel. 🔴 **BİR CÜMLESİ YANLIŞTI, ORKESTRATÖRÜN**: *"M3-06: `tap:*` → `review`, diğer her eylem → `deny`"* — gerçek: `evaluate.go:431` `reviewDefaultActions = { ActionTapRecord: true }`, yani **`tap:approve` `deny`'a düşüyor**, ve motorun kendi yorumu bunu *"the ADR's loose `tap:* → review` shorthand"* diye adlandırıyor. Yani kartın **"`tap:approve` tuzağı"** diye uyardığı okuma hatasını devir notunda **ben tekrarlamışım**. ⚠️ **Yapıcı tuzağa DÜŞMEDİ** — ayrımı isimden değil **motora sorarak** türetti (`policy.Evaluate(Set{}, Context{Action: a})`) ve güvenlik merceği bunun **boş Set ile gerçek Set'te aynı** cevabı verdiğini ölçtü. Zarar oluşmadı.)* **M6-09 — Policy yönetim ekranı.** **Bağımlılık:** M6-02 · M3-06, **ikisi de done**. **Kırmızı çizgi: §4** (guardrail'ler ekranda **görünür ama kapatılamaz**). **Araç:** skill `tappa-brand`. **Q22:** v1 kapsamı **dar** — yalnız **form** arayüzü; ham JSON editörü (M9-07) ve simülatör (M9-06) **pilot sonrasına ertelendi**. 🔴 **M6-09'UN BİLMESİ GEREKEN ALTI ŞEY:** **(1) 🔴 BU, PANELİN POLICY MOTORUNU ÇAĞIRAN İLK EKRANI OLACAK — ve iki görev üst üste onu BİLİNÇLİ OLARAK ÇAĞIRMADI.** M6-07 B (`report:export`) ve M6-08 (`record:manual`) ikisi de **ölçüp geçti**: panelde `policy.Evaluate` çağıran **hiçbir handler yok**, baseline **owner ve manager'ın ikisine de** veriyor (yani rol kapısı **kimseyi reddetmez**), ve ⚠️ **`internal/domain/checkin/policyset.go`'nun `forTenant`'ı UNEXPORTED OLMAKLA KALMIYOR, BASELINE'I MATERIALISE EDİYOR** — bir panel isteği **policy tablolarına satır yazardı**. **Bu senin çözmen gereken mimari sorun**: okuma yolu için materialise etmeyen bir yol mu, yoksa panelin kendi Set assembly'si mi? **Ölç ve iki okumayı önüme koy.** **(2) GUARDRAIL'LER EKRANDA GÖRÜNÜR AMA KAPATILAMAZ** — kapatma kontrolü **hiç yok**, disabled da değil; kilit + gerekçe + **hangi kırmızı çizgi**. Sırası da gösterilecek: **ADR 0007** 4–7 bandını değiştirdi, **kartın ilk tablosu değil M3-05'in 2026-08-02 düzeltme bloğu** doğru kaynak. **(3) `record:manual` ve `report:export` ARTIK GERÇEK EYLEMLER** (`policy/document.go`) ve **hiçbiri panelde zorlanmıyor** — bu ekran onların yüzü; **yetkilendirme politikaları ayrı bölümde ve varsayılanın fail-closed olduğu ekranda YAZILI olmalı** (M3-06: `tap:*` → `review`, diğer her eylem → **`deny`**). **(4) SÜRÜM GEÇMİŞİ: düzenleme YENİ SÜRÜM üretir, üzerine yazmaz** — `policy_versions` append-only, ve **M3-07'nin `policy_context jsonb`'si 1. günden beri dolu** (M9-06 simülatörü ona dayanacak). Her değişiklik `audit_log`'a. **(5) M6-06/M6-07/M6-08'İN KALIPLARI — ama YARISINI kopyalama** (bu projede **beş kez** kusur oldu): `ProtectWriting` · audit **`RecordTx` ile aynı transaction'da** (M6-07 B `Record` kullandı çünkü **okuma**; M6-08 `RecordTx` çünkü **yazma**) · MAC'li onay v3 (**altı eylem**, matris `go/ast` ile **bildirimlerden türetiliyor**) · C′ · rota kümesi `chi.Walk` ile **türetiliyor** (bugün **13 yazma rotası**). **(6) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-10):** `make test` **2597 PASS / 0 FAIL / 0 SKIP**, **17 paket**; **çıplak `go test` DB testlerini SESSİZCE atlar**; `make check` **`gen` de koşuyor** ve **yalnız** son `git diff --exit-code`'da durur. Son migration **00014**. ⚠️ **Türetilmiş §4.5 kuşak ağının KÖR NOKTASI var: `INSERT … VALUES`'ü hiç göremiyor** (ürün genelinde **7 sorgu**, `InsertTransaction` dahil) ve **INSERT'ün yazdığı `tenant_id`'ye hiç bakmıyor** — backlog **T20**. **Canlı iddia olarak sayı yazma.** |
| **~~M6-08 devir notu~~** | *(2026-08-10'da teslim edildi; tarihsel. 🔴 **BEŞ CÜMLESİ YANLIŞLANDI, BEŞİ DE ORKESTRATÖRÜN**: `sun_valid=false` → **NULL** · **`RecordTransaction` diye bir sorgu YOK**, adı `InsertTransaction` (M6-07'nin güvenlik denetçisinden **doğrulamadan** taşınmıştı) · zemin `2493 PASS / 16 paket` → **2597 / 17** · migration `00013` → **00014** · ve *"geriye tarihli manuel giriş artık doğru geç kalma üretir"* — **doğru sonuç, yanlış mekanizma**: manuel kayıt `tap.Decide`'a **hiç uğramıyor**, geç kalma raporda `arrival()` içinde hesaplanıyor. ⚠️ **Ayrıca orkestratörün bir DÜZELTME TALİMATI da yanlıştı**: yapıcıya *"kuşak ağı 27/69 → 27/70"* dedi; denetçi ölçtü — `git show HEAD:db/queries/*` → **69**, yani **69 devir anında DOĞRUYDU** ve 70 ancak M6-08 kendi sorgusunu ekledikten sonra oldu.)* **M6-08 — Manuel kayıt girişi.** **Bağımlılık:** M6-03, **done**. **Kırmızı çizgi: §4.3 · §4.6.** **Araç:** skill `tappa-brand`. 🔴 **M6-08'İN BİLMESİ GEREKEN YEDİ ŞEY:** **(1) 🔴 BU, ÜRÜNÜN `transactions`'A İKİNCİ YAZICISI — VE M6-07'NİN ÖLÇTÜĞÜ BİR BARİYERİ TAŞIYICI HÂLE GETİRİYOR.** Faz A `endpointState`'in `default` dalını fail-safe yaptı, çünkü şemada *"reject/ignored ⟹ yön yok"* diye bir CHECK **YOK** — tek kısıt **tek yönlü** (`transactions_ok_has_direction`: `verdict <> 'ok' OR type IS NOT NULL`), ve rollback'li bir sonda `verdict='reject', type='in'` satırını **kabul ettirdi**. Bugün ulaşan yol olmamasının **tek sebebi** `internal/domain/tap/decide.go:239`'un yönü yalnız `ok|flag` dallarında set etmesi — **bariyer bir KOD değişmezi, şema kısıtı değil**. `RecordTransaction` **`@type`'ı ÇAĞIRANDAN alıyor**; M6-08 o `if`'i atlayan **ikinci yol** olursa bariyer düşer. **Yönü kim belirliyor, hangi verdict'le, ve şemaya bir CHECK gerekiyor mu → ÖLÇ ve bana bildir** (migration ister). **(2) `channel='manual'` + `entered_by` (giriş yapan admin) + `sun_valid=false` + trust TABAN puanı.** §5: manuel kayıt **debounce ÖNCÜLÜNDEN MUAF** — *"müdürün yazdığı satır dokunuş değildir"* (ADR 0006). **(3) Geçmişe dönük kayıt mümkün ama `created_at` GERÇEK yazım anını gösterir** — `occurred_at` ile `created_at` ayrı alanlar. ⚠️ Faz A `tap.Decide`'ın lateness'ini **`occurred_at`'e** bağladı, yani geriye tarihli bir manuel giriş artık **doğru** geç kalma üretir (eskiden **−520** dakika veriyordu). **(4) 🔴 DÜZELTME = YENİ SATIR + AUDIT, UPDATE YOK (§4.3)** — ve `transactions` **DB düzeyinde** append-only, tetikleyici **`tappa_owner`'ı da bağlıyor** (güvenlik merceği bu oturumda yeniden ölçtü: `tappa_forbid_mutation()` DELETE'i reddetti). **(5) Q18'İN KAPANIŞ UCU BURASI:** sistem çıkış **üretmez**, açık kayıtları **müdür manuel kapatır**. M6-07'nin *"open / needs action"* bölümü ve CSV'si bu akışın **girdisi**; yazdığın satır **anında** saat motoruna girer ve raporda `manual` olarak **ayrı işaretlenir**. **(6) M6-06/M6-07'NİN KALIPLARI GEÇERLİ — ama YARISINI kopyalama** (bu projede **beş kez** kusur oldu; sonuncusu M6-07 B'de bir kaçış sınıfının yarısının devralınmasıydı): **`ProtectWriting`** (`floodGate → sameOriginGate → requireAdmin → sessionGate`, `Protect`'in üst kümesi) · audit **`RecordTx` ile AYNI transaction'da** — ⚠️ M6-07 B `Record` kullandı çünkü **okuma** yoluydu, **M6-08 bir YAZMA**, ikisini karıştırma · geri alınamaz eylem → **MAC'li onay v3** · bir eylemi ilan eden başlık **C′** ile **aynı isteğin okuduğu satıra** doğrulanır · **kapalı sözlükler ve rota kümeleri TÜRETİLİR** (`go/ast` + `chi.Walk`). **(7) ⚠️ ÖLÇÜM ZEMİNİ (2026-08-10):** `make test` **2493 PASS / 0 FAIL / 0 SKIP**, 16 paket; **çıplak `go test` DB testlerini SESSİZCE atlar** (bu oturumda bir denetçi buna düştü ve yeşil sandı); `make check` **`gen` de koşuyor** ve **yalnız** son adım `git diff --exit-code`'da durur. Son migration **00013**. Kuşak ağı **27/69**. **Canlı iddia olarak sayı yazma.** |
| **~~Faz B devir notu~~** | *(2026-08-10'da teslim edildi; tarihsel. ⚠️ İki cümlesi kapanışta yanlışlandı, **ikisi de orkestratörün**: `make test` **2416 PASS** → bugün **2493**; ve `ReportEventCap` **1,64×** → bugün **1,58×**. İkincisi **yanlış değil bayattı** — doğru popülasyondan geliyordu, ve bir denetçi onu **farklı bir popülasyonla** çürütmeye çalışıp kendi hatasını yaptı.)* **M6-07 FAZ B — CSV export.** Faz A **done** (`671289b`, üç denetçi ONAY). **Yeni migration YOK** — A eklemedi ve kendi `EXPLAIN`'i istemiyor; B'nin de istemesi beklenmiyor, isterse **ölçüp bildir**. 🔴 **B'NİN MİRAS ALDIĞI SEKİZ YÜKÜMLÜLÜK** (kartın 2026-08-10 düzeltme bloğunda, üç denetçi de doğruladı): **(1) ARİTMETİĞİ YENİDEN YAZMA** — CSV aynı `ledger.Report`'tan üretilir; ikinci temsil bu repoda **beş kez** bedel ödetti. **(2) CSV kaçışı `= + - @`** — templ kaçışı CSV'ye **geçmez**, ve bizim ürettiğimiz `Unknown employee` gibi dizeler de dahil. **(3) §4.7:** sorguya koordinat sütunu **ekleme** — A'nın duvarı sorgunun kolon listesi; matcher **ad tabanlı** ve `Detail`/`Extra`/`Meta` gibi nötr adlı bir alanı **görmez**. **(4) `audit_log`:** A hiçbir şey yazmıyor, B'nin **tek yazışı** bu olacak — toplu veri çıkışının dürüst kontrolü **kimin ne indirdiğinin kaydı**. **(5) `report:export`:** eylem `policy/document.go:135`'te **tanımlı** ama panelde `policy.Evaluate` çağıran **hiçbir handler yok** (grep boş; motorun tek entegrasyonu tap yolunda). Rol kapısı da **bugün kimseyi reddetmez** (baseline owner **ve** manager'a veriyor). Ya oku ya **açıkça yaz**; gerçek kapı **M6-09**'un işi. **(6) UTC **ve** yerel, ISO 8601**, hangisi olduğu **başlıkta açık**, UTF-8. **(7) `ReportEventCap` kesilmesi CSV'de de görünmeli** — bugün **1,64×** en yoğun ölçülen hafta (20 000 / 12 193), yani ulaşılabilir. **(8) `maxReportRows`/`maxOpenRows` CSV'de OLMAMALI** — ekran listesi 100'de kesiliyor ve *"gerçek cevap CSV'dir"* denerek bırakıldı. ⚠️ **VE Q18 B'DE DE GEÇERLİ:** açık girişler saate **girmez**, `practice` o bölüme **girmez**, ve CSV bir *"floor"*u **toplam gibi** sunamaz. 🔴 **ÖLÇÜM ZEMİNİ (2026-08-10, üç denetçi bağımsız ölçtü):** `make test` **2416 PASS / 0 FAIL / 0 SKIP**, 16 paket; **çıplak `go test` DB testlerini SESSİZCE atlar** (bir denetçi bu oturumda bizzat düştü: `DATABASE_URL not set` → yeşil sandı) — `make` `.env`'i `-include`+`export` ile yüklüyor. `make check` **`gen` de koşuyor** ve **yalnız** son adım `git diff --exit-code`'da durur. Kuşak ağı **dosyadan türetiyor**, bugün **27/69** (kaynak `internal/domain/tenant/query_test.go:209`); **canlı iddia olarak yazma**. ⚠️ **T17 M6-07'YE DEĞMEDİ:** A `audit_log`'a **hiç dokunmadı**; ama uyarının **şekli** tuttu — `ListWorkedShiftEvents` tam o şekle düşüyor (tenant-kapsamlı bitmap scan + haftayı süzen filtre, **17 323 satır elenerek**). **B `audit_log`'a YAZACAK**, yani T17'nin okuma tarafı yine onun işi değil. ⚠️ **M6-06'NIN KALIPLARI B İÇİN GEÇERLİ, A İÇİN DEĞİLDİ:** `ProtectWriting` (dört aşama) · audit **`RecordTx` ile aynı transaction'da** · geri alınamaz eylem → **MAC'li onay v3** · başlık **C′** ile doğrulanır · **kapalı sözlükler ve rota kümeleri TÜRETİLİR** (`go/ast` + `chi.Walk`). B `audit_log`'a yazdığı an bunların **hangisi gerekiyor, hangisi gerekmiyor** — **yarısını kopyalama**, bu projede **dört kez** kusur oldu. |
| **~~Eski devir notu~~** | *(2026-08-10'da yenilendi; aşağıdaki altı madde M6-07'nin tamamı için yazılmıştı ve A kapandığında dördü yanlışlandı — denetçi bulguları, orkestratörün hatası. Tarihsel kayıt olarak duruyor.)* 🔴 **M6-07'NİN BİLMESİ GEREKEN ALTI ŞEY:** **(1) KART Q18'İ TAŞIYOR VE ÇOK AYRINTILI — OKU.** Sistem **otomatik çıkış üretmez**; açık kalan girişler saate **girmez**, ayrı bir *"open / needs action"* bölümünde listelenir ve rapor **toplamın eksik olduğunu açıkça söyler**. ⚠️ **`practice = true` girişler o bölüme GİRMEZ** (M5-07 denetimi) ve ⚠️ o bölüm **ADR 0008'den sonra daha az satır görür** — ama bir kaydın **neden** açık kaldığını (unutulmuş çıkış mı, maskelenmiş giriş mi) **satırın kendisinden söyleyemez**. **(2) `float` YOK (§6).** Saat/para `numeric`/`Duration`. CSV: **UTF-8, ISO 8601, saatler UTC VE yerel, hangisi olduğu başlıkta açık.** **(3) `manual` kayıtlar ayrı işaretli**, `practice` saate **dahil değil**, geç kalma **çalışanın KENDİ vardiyasına** göre (M4-05). **(4) M6-06'NIN KALIPLARI HAZIR — ama YARISINI kopyalama** (bu projede **dört kez** kusur oldu): mutasyon rotası `ProtectWriting` (**dört aşama**) · audit **`RecordTx` ile aynı transaction'da** · geri alınamaz eylem → **MAC'li onay v3** (`action|subject|session`, **beş eylem**, matris **bildirimlerden türetiliyor**) · bir eylemi ilan eden başlık **C′** ile **aynı isteğin okuduğu satıra** doğrulanır · **kapalı sözlükler ve rota kümeleri TÜRETİLİR** (eylem sözlüğü `go/ast` ile, rotalar `chi.Walk` ile — ikisi de M6-06 B'de sevk edildi ve **Faz A'nın dört rotasını da** kapsadı). **(5) 🔴 CSV BÜYÜK BİR ÇIKTI YÜZEYİ — ve M6-03'ün dersi burada tekrar edecek:** filtre çubuğu bir kez sayfanın **%96'sını** yemişti. Bayt maliyetini **ölç**, ve **T17'ye dikkat**: `audit_log`'da `(tenant_id, target)` indeksi **yok** ve rapor sorguları benzer bir tarama şekline düşebilir — `EXPLAIN (ANALYZE, BUFFERS)` ile ölç ve **kuralı uygula: bir zamanlama, TENANT'I + SATIR SAYISI + İŞLEM SAYISIYLA yazılır, ya da hiç yazılmaz.** **(6) ⚠️ ÖLÇÜM ZEMİNİ:** `make test` **2335 PASS / 0 SKIP**; **çıplak `go test` DB testlerini SESSİZCE atlar** (son denetimde **333 alt test**) — `make` `.env`'i `-include`+`export` ile yüklüyor. `make check` **`gen` de koşuyor**. Kuşak ağı **dosyadan türetiyor**, bugün **27/67**; **canlı iddia olarak yazma**. |
| **Çalışma modu** | Orkestrasyon + üçüncü göz — [README.md](README.md) · brief'ler [agent-brief.md](agent-brief.md) |
| **Dal** | **`main`** — M0 (`m0-bootstrap`) `main`'e fast-forward birleştirildi (`562f021`), dal silindi. **Kullanıcı kararı (2026-07-25): artık doğrudan `main`'de çalışılır, görev başına dal açılmaz** (CLAUDE.md §10 güncellendi). Push/PR yine istemedikçe yok. |
| **Blokeler** | 🔴 **ÜÇ KULLANICI EYLEMİ — ÜÇÜ DE DEPLOY'U TAMAMEN BLOKLUYOR, ve üçü de FAZ D'den doğdu.** **(1)** `kubectl apply -f deploy/k8s/00-namespace.yaml` sonra `01-rbac.yaml`, **cluster-admin kubeconfig ile, bir kez** — deploy kendi RBAC'ini **uygulayamaz** (`rbac.authorization.k8s.io` yetkisi yok, **doğrusu da bu**); `apply` kullan, `delete`+`create` **değil** (`KUBE_CONFIG` o ServiceAccount'a ait). **(2)** GitHub repo secret'ları **`DOCKERHUB_USERNAME`** + **`DOCKERHUB_TOKEN`** (Docker Hub → Account Settings → Personal access tokens, **Read & Write**) — ölçüldü: `gh secret list` bugün **yalnız `KUBE_CONFIG`**. **(3)** `atknatk/tappa` ve `atknatk/tappa-migrate` depolarını Docker Hub'da **Public olarak yarat** — ölçüldü: ikisi de **404, yani hiç yok**, ve **var olmayan bir depoya ilk push onu PRIVATE yaratır**, ki bu KEP-2535 kusurunu (pod açılamaz) **doğrudan geri getirir**. ⚠️ Yeni kapı bunu artık **ölçüyor** ve depolar public olana kadar deploy'u geçirmiyor. **Ayrıca ÖLÇÜLEMEYEN bir sınır:** `KUBE_CONFIG` secret'ının **içindeki kimlik doğrulanamadı** — hâlâ cluster-admin bir kubeconfig ise `01-rbac.yaml`'ın **tüm daraltması etkisizdir**, çünkü cluster-admin RBAC'e bakmaz; kapatan komut `deploy/README.md` operatör adımı 5'te. **Diğer bekleyen kullanıcı eylemleri → [docs/backlog.md](../backlog.md)** (B1 iPhone/Q11 ölçümü, B2 arm64 Go kurulumu) — **ikisi de hiçbir şeyi bloklamıyor**. Q02 (davet kanalı) M5-02'yi bloklamaz; kart cevapsız hâli için yol gösteriyor. |

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
- **🔴 N5 (M4-03 denetiminden) — M5 için BLOKLAYAN §4.5 tenant izolasyonu:** `tap.Decide` sıfır tenant-farkındalığıyla
  çalışıyor — `Input`'ta bugün **TagTenantID/SessionTenantID YOK**, dolayısıyla `sys:tenant-mismatch` guardrail'i
  ölü. Kritik incelik: tag çözümü (`GetTagByUID`) **context-less**'tır (ADR 0002 md.7) → **RLS çapraz-tenant tag'i
  çözümde GİZLEMEZ.** Tenant B çalışanı fiziksel olarak tenant A'da (IP/GPS eşleşir) A plaketine dokunursa,
  `sys:tenant-mismatch` beslenmediği sürece **`ok` check-in yazılır (izolasyon deliği).** Tek savunma bu
  guardrail'in beslenmesidir. **M5, `Input`'u `TagTenantID`+`SessionTenantID` ile genişletip Decide'a vermeli**
  (tag çözümünden tenant + oturumdan tenant). M4-03 bunu doğru şekilde M5'e erteledi (Decide karar taklidi
  yapmıyor); ama M5 bunu sağlamazsa delik açık kalır — **belt-and-braces değil, tek gerçek engel.** Ayrıca (düşük):
  Decide her redirect'i `RedirectActivation`'a eşliyor → M5 tenant-mismatch redirect'ini aktivasyondan ayırabilir.

### M6-02 / M6-05 / M7-02 / M7-04 / M8'e devralınan (M6-01 B'nin ON İKİ LİMİTİ)

Hiçbiri kapatıldı diye yazılmadı; hepsi **ölçüldü ve sayıldı**. Sahibi belli olanlar işaretli.

1. **🔴 M6-05 / M7-04 / M7-02 — digest-tarafı zamanlama kolu, ve SEBEBİ SÜRESİ DOLACAK.** Geçerli bcrypt
   digest'i olmayan bir admin satırı **154–198 ns**'de cevap veriyor (bcrypt anahtar programını kurmadan
   hata döner), kukla kolu **297,9 ms** → **~1,5–1,9 milyon×**. Yani 53× kehanetinin şekli **digest
   tarafından ve ters yönde** yeniden açılır. **Bugün erişilemez** (`db/queries` ve üretim Go'sunda
   `INSERT INTO admin_users` yok, `password_hash` UPDATE'i yok — tek `UPDATE admin_users`
   `MarkAdminLoggedIn`/`last_login_at`; seed'in iki satırı da `$2a$12$`; `Hash` boşu ve >72'yi reddediyor)
   **ama şemada format CHECK'i YOK**, `''` şema-geçerli. **Kural: `admin_users.password_hash` yalnız
   `adminauth.Hash` çıktısıyla yazılır.** Yapısal çözüm bir sütun CHECK'i = **yeni migration**, alınmadı.
2. **✅ ÖDENDİ (M6-02, `6757537`) — borç M6-03'e EL DEĞİŞTİRDİ, iptal edilmedi.** Bu üç madde
   *"`adminSessionLimit` kopyalandı"* · *"pay 1,5×"* · *"`adminFloodLimit` 10 parça ≈ 2000 varsayımıyla"* ·
   *"`sessionGate`'in sınırladığı iş boş"* diyordu. **Dördü de ölçümle çürütüldü:** M6-02 **HTMX getirmedi**
   (kullanıcı kararı: M6-03) ve gerçek sunucuda **bir sekme görüntülemesi = TAM 1 ücretli istek**
   (`/static` kapının **dışında**; 305 ardışık `GET /admin` → **300×200, #301'de 429**). Ölçülen meşru yük
   **~260/pencere** (200 görüntüleme + ~60 giriş), pay **11,5× (3000/260)** — eski öncülün ima ettiği
   **1,46× (3000/2060)** değil. ⚠️ **`300/20 = 15×` AYRI bir tavandır** (oturum başına, yönetici-başına
   payda) **ve doğrudur** — ikisini karıştırma; bu iki *"15×"*ten yalnız biri hataydı. **Üç sabit de
   DEĞİŞMEDİ.** Türetme artık `adminratelimit.go`'da, tavanların **yanında** yaşıyor (yalnız plan kartında
   değil) ve **paydayı gösteriyor**. **M6-03 parçaları getirince çarpan geri gelir → YENİDEN SAY**; eşik
   yazılı: **≥15 istek/görüntüleme**.
3. **M6-02 — `adminFloodLimit = 3000`'in maliyeti (referans, ödendi).** 12. turda kapı `Protect`'in
   **önüne** kondu (F-A'yı kapatmak için), böylece anonim kalkan ile meşru iş **tek kovayı** paylaşır oldu;
   tavan 200 → 3000 çıkarıldı. Maliyet **doğru kolda** ölçüldü: canlı oturum **3,0–5,7 ms** (resolver
   okuması **+ `TouchAdminSession` UPDATE**), uydurma token 0,65–1,21 ms →
   `3000 × 3,0–5,7 ms = 9–17 sn/pencere/adres = bir çekirdeğin %1,5–2,9'u`.
4. **M6-03 — `sessionGate` (300/oturum) kimlik doğrulamadan SONRAKİ işi sınırlar; M6-02 onu BEŞ ROTAYLA
   DOLDURDU ama her biri tek istek.**
   Ve **koruduğu maliyetin yanlış tarafında**: gate `requireAdmin`'den sonra koştuğu için **429 alan
   istek resolver okumasını ve UPDATE'i zaten ödemiştir** (ölçüldü: reddedilen istekte bile `last_used_at`
   değişiyor). Taşımak mümkün değil — oturumu çözmeden oturuma göre anahtarlayamazsın; `tap.go`'da sorun
   yok çünkü `httpx.Identify` bilerek **yazmıyor**.
5. **Çıkış 30000 üçüncü-taraf isteğinde reddedilebilir — invaryant ZAYIFLADI.** 14. turda çıkış
   *"asla reddedilmez"*di; 16. turda kendi tavanı kondu (`adminLogoutLimit = 10 × adminFloodLimit`) çünkü
   ölçüldü ki *"asla reddetme"* **sınırsız** bir yükselteç demekti (10000 anonim çıkış → **10000 resolver
   okuması, 0 red**, tap yüzeyinin **paylaştığı havuzda**). Şimdi üçüncü tarafın kurbanın çıkışını
   engellemesi **30000 istek** gerektiriyor — panelin geri kalanını reddetmenin **10 katı** ve flood
   log'unda gürültülü. **Bedeli:** `30000 × 0,65–1,21 ms ≈ 19,5–36 sn/pencere/adres ≈ çekirdeğin %3,3–6,0'ı`
   — **ürünün en geniş tavanı, flood tavanından pahalı.**
6. **Bastırılan deneme TOPLAMI DB'den kurtarılamaz.** Hesap audit bütçesi bir **iz susturma** primitifi:
   60 başarısızlık → **11 satır** (10 `failed` + 1 `rate_limited`). `rate_limited` satırı `SuppressedFrom`
   taşıyor (bastırmanın **başladığı sıra**, ölçüldü: 11) → *sessizlik değil kesinti*; müfettiş saldırıyı
   görür, **sayısını göremez**. Gerçek sayım pencere **kapanışında** bir satır ister; `httpx.Limiter`
   tembel tahliye ediyor ve süre-sonu kancası yok → **altyapı**.
7. **`-race` zamanlama kapısı 2,5× altını GÖRMEZ.** Kapı ölçülmüş gürültüye göre genişletildi (kullanıcı
   kararı 2026-08-03) ve **bir cost-adımlık kuklayı (1,91–1,99×) bilerek feda ediyor** — o vaka
   `TestCost_MatchesTheDummyDigest`'in **tam sayı** karşılaştırmasına bırakıldı. Gerçekten korumasız olan:
   ne eksik kukla ne cost uyuşmazlığı olan, **2,5× altındaki dördüncü bir şekil**.
8. **Sunucu tarafı panel oturum süresi YOK.** `admin_sessions`'da `expires_at` yok; 12 saatlik `Max-Age`
   bir **tarayıcı ipucu**. Gerçek kontroller: açık çıkış · *"her yerden çıkış"* (**rotası mount edilmemiş**)
   · `admin_users.status='disabled'` (**⚠️ M6-05 DEĞİL → M7-04'e taşındı**, orkestratör kararı 2026-08-08:
   M6-05 **Employees** sekmesidir, `admin_users` başka bir varlık ve **M7-04 = *"Admin daveti, şifre
   sıfırlama"*** o yaşam döngüsünün sahibi. ⚠️ **Ölçüldü: sütun ve CHECK ZATEN VAR** (00006) ve
   `TouchAdminSession` `admin_users`'a join edip `a.status='active'` **test ediyor** → kill switch **canlı**,
   eksik olan yalnız **onu set eden yüzey**. Yani *"düzeltmesi migration"* bu yarıya değil, **`expires_at`'e**
   aitti). `admin_sessions.expires_at` düzeltmesi hâlâ **migration**.
9. **Bilinmeyen e-postalı denemeler AUDIT'LENEMEZ.** `audit_log.tenant_id` NOT NULL + FK → atfedilecek
   tenant yok. Sıfırın **mekanizması** `failLogin`'in 0 adayla döngüye hiç girmemesi; **kısıt** boşluğun
   neden kapanmadığının gerekçesi. Kart kriteri bu yüzden **⚠️ KISMEN** işaretli.
10. **`GO-2026-5932` kabul edildi.** `golang.org/x/crypto/openpgp` bakımsız, **`Fixed in: N/A`**;
    yalnız `bcrypt` import ediliyor (4 satır, dördü de bcrypt), **0 vuln kodu etkiliyor** → `make audit`
    yeşil. **Yükselterek kapatılamaz.**
11. **Tip invaryantı AD-TABANLI.** `TestPackageTypes_NoExportedCredentialField` paketteki dışa açık
    struct'ları gezip **sır-benzeri ADI** olan ham alan arıyor (sabit listeli önceki hâli yeni **tipe**
    karşı çaresizdi — negatif kontrolle kanıtlandı). **Nötr adlı bir alan (`Value string`) kaçar.**
12. **🔴 M8 — SUITE GENELİ BAĞLANTI TÜKENMESİ, ÖNCEDEN VAR, M6-01 SEBEP DEĞİL.** `max_connections=100`
    − 3 rezerve = **97 slot**; `internal/db/invites_test.go` ve `internal/sun/advance_test.go`'nun
    kırmızı-çizgi yarış testleri **54'er** bağlantı açıyor (50 goroutine + 4) = **108 > 97**, yani
    **tek başlarına** sınırı aşıyorlar. Belirti: `TestConsumeInvite_ConcurrentRaceExactlyOneWinner` →
    `FATAL: sorry, too many clients already`. ⚠️ **Goroutine sayısını düşürmek bir §4.4 testini
    ZAYIFLATIR** (aynı `(tag, ctr)` ile N goroutine → tam 1 kazanan) → düzeltilmedi. Çözüm ya
    `max_connections` ya testcontainers ile izole DB — ikisi de **altyapı**.

### M6 / M7'ye devralınan (M5-11 denetimlerinden)

- **✅ M6-04'ün yarısı ÖDENDİ (`2e7ec64`); kalan yarı M6-08'in. VE BU MADDENİN İLK HÂLİ FAZLA GENİŞTİ.**
  Eskiden *"`audit_log` YOLU YOK"* diyordu; **yanlıştı** — `internal/audit` paketi M6-01 B'den beri var
  (`Record` = kendi transaction'ı, `RecordTx` = çağıranınkini paylaşır) ve dev DB'de o zaman bile **15
  gerçek action'da ~20.000 satır** vardı. ⚠️ **Doğru ifade DAR olandır ve M6-04 onu ikiye böldü:**
  (a) `review%` action'ı **0'dı → bugün 652**, review akışı `transaction_reviews` + `audit_log`'a tek
  transaction'da yazıyor — **bu yarı kapandı**; (b) `channel='manual'` işlemini **hedefleyen** `audit_log`
  satırı **hâlâ 0** ve manuel giriş HTTP rotası **hâlâ yok** → **sahibi M6-08**. Var olan: `checkin.Service.
  Record` domain yolu (`ErrEnteredByRequired`). ⚠️ **Sayı da eskimişti:** `channel='manual'` **408 değil,
  2026-08-07'de 4.902** — append-only olduğu için her suite koşusunda büyüyor, yani **bu satıra bir
  büyüklük demirlemek yanlıştır**; ölçmek istersen komut: `SELECT count(*) FROM transactions WHERE
  channel='manual'`. ⚠️ `redline-check.sh` bunun **yokluğunu yakalayamaz** (audit_log'a bakmıyor), yani
  tek koruma bu satırdır.
- **🔴 M6-11 — maskelenmiş açık girişler ANOMALİ LİSTESİNDEN KENDİLİĞİNDEN DÜŞÜYOR.** `NOT EXISTS`
  `o.occurred_at > t.occurred_at` olduğu için **tek bir `out` kendinden eski TÜM açık girişleri kapatıyor**
  → maskelenmiş bir giriş, kişinin bir sonraki çıkışıyla "kapanmış" görünüyor ve o aralık **saat toplamına
  yanlış** giriyor. Ölçüldü: dev DB'de **47 hâlâ görünür, 43 zaten görünmez**. Geriye dönük tespit
  `p.practice AND p.occurred_at > t.occurred_at` sorgusunu gerektirir. **§4.6 ihlali DEĞİL** — hiçbir satır
  kaybolmuyor, kaybolan **sinyal**.
- **🟡 M5-05 K1 torbası — GERİYE TARİHLİ `out` HİÇBİR ZAMAN KAPATMIYOR.** Ölçüldü (practice'ten
  **bağımsız**, M5-11'in eseri **değil**): `in @13:16` altına `out @12:16` ve `out @11:16` yazılabiliyor,
  ikisi de **dangling** kalıyor, `in` **sonsuza dek açık** (dev DB'de **84** dangling `out`). Hafifletici:
  her geriye tarihli satır `base:queued-window`'u (120 sn, tenant ayarlı) aşıyor ve **flag** alıyor.
- **🟡 M7/M8 — `GetLastOpenTransaction` KİŞİ BAŞINA O(ÖMÜR BOYU SATIR) tarıyor** ve `practice` indekste
  **yok** (`Filter`, `Index Cond` değil). **DoS değil** (ölçüldü, üç ölçekte: sıradan şekilde yüklem
  **bedava** — 11 300 → 11 300 buffer; practice-en-üstte şekli bile sıradan şeklin **altında** kalıyor;
  `Timeout(30s)`'e ulaşmak kişi başına **~3 milyon satır** = iki aylık kesintisiz flood, ve zarar
  **saldırganın kendisiyle** sınırlı — kişi-bazlı advisory kilit). Ama §4.3 satırları kalıcı kıldığı için
  taranan küme **yalnız büyür**. Kısmi indeks bir **migration**'dır.
- **⚪ `make audit` üretilen kodu TARAMIYOR** ve bu **bilinçli**: `redline-check.sh` `GEN_EXCLUDE` ile
  `internal/store/*.go`'yu dışlıyor; **kaynak** (`db/queries/`, `SRC` içinde) taranıyor. Savunulabilir —
  ama R kuralları **kırmızı çizgi desenleridir**, yani *"bu rapor sorgusu `NOT practice` taşımalı"* gibi
  **anlamsal** bir kural **hiçbir katmanda** kontrol edilmiyor.

### M5-11 / M6 / M7 / M8'e devralınan (M5-10 denetimlerinden)

- **🔴 M6/M7 — GUARDRAIL SIRASI HÂLÂ ELLE BAKIMLI, ve ağın sınırı yazılı.** ADR 0007 sırayı
  düzeltti ve **yapısal** bir invaryant getirdi (`TestGuardrails_NothingUnnamedPreemptsAnAlert`:
  *uyarı taşıyan bir guardrail'in önündeki her şey adlandırılmış istisna olmalı*), iki kalıcı
  negatif kontrolle (`…RejectsTheRegressionOrder`, `…RejectsAnUnnamedEleventhGuardrail`).
  **Kalan boşluk yazılı:** istisna listesini (`namedAlertPreemptors`) küçültmek hâlâ mümkün —
  ama artık **görünür ve tartışılır** bir düzenleme. Yeni guardrail ekleyen herkes bu invaryantı
  okumalı; *"kırmızı testin listesini güncelle"* **yanlış onarımdır**.
- **🔴 M6/M7 — `sys:no-session` istisnasının gerekçesi BAŞKA PAKETTE yaşıyor.** `sys:no-session`
  (4) uyarı taşıyan `sys:employee-deactivated`'in (5) önünde ve bastırma **boş** — çünkü
  `internal/domain/tap/decide.go` `employee:status` anahtarını **ve** `SessionTenantID`'yi aynı
  `Employee != nil` dalında koyuyor, yani ikisi karşılıklı dışlayıcı. ⚠️ **`policy.Evaluate`
  DIŞA AÇIK bir API:** `employee:status`'ü `SessionTenantID`'siz set eden **ikinci bir çağıran**
  bunu boş bir ön-almadan **gerçek** bir ön-almaya çevirir. `internal/policy` bunu **zorlayamıyor**;
  sınır `guardrails.go`'da yazılı, ama pinlenmiş değil.
- **🔴 M6/M7 — `>900 sn` bandında NE KAYIT NE UYARI var** (ADR 0007 garanti-dışı #5). Ölçüldü:
  `GET /t` (deaktif) → **400**, `transactions +0`, **sıfır audit aksiyonu** → tenant tarafında
  **hiç iz yok**. Regresyon **değil** (M5-10 öncesi bant aynıydı) ve düzeltme kesin olarak
  iyileştirdi (uyarıyı düşürmek için gereken bekleme **3 dk → 15 dk**), **ama asimetri gerçek:**
  kapatılan yol satırı **yazıyordu**, hayatta kalan yol **hiçbir şey** yazmıyor — yani *"deaktif
  çalışanın denemesi kaydedilir"* yükümlülüğü 15 dakika beklemekle atlatılabiliyor. Sahibi:
  **M6/M7 uyarı teslimatı** (bugün `tap.security_alert`'in üretimde **hiçbir okuyucusu yok** —
  ADR garanti-dışı #3: satır garanti, **teslimat değil**).
- **🟡 M6/M7 — `retired` plakette uyarı hâlâ düşüyor** (garanti-dışı #1). `sys:tag-not-active` (2)
  `lost`'ta uyarıyı **daha acil** olanla takas ediyor (`lost-tag-tapped`) ama `retired`'da hiç
  üretmiyor. Güvenlik denetçisi §5 satır 4 ihlali **saymadı** (etiket durumu sunucu gerçeğidir,
  istemci seçemez; §5 satır 1 zaten satır 4'ün önünde) — **kabul edildi ve yazıldı**.
- **🟡 M8 — `floatEnvRange` `NaN`'ı SESSİZCE geçiriyor** (`config.go`). Ölçüldü: `NaN < 60 == false
  && NaN > 900 == false` → aralık kontrolü geçiliyor, `time.Duration(NaN×1s)` **int64 minimuma**
  dönüyor. **Freshness için M5-10'un eklediği kontrol yakalıyor** (başlangıçta patlıyor), ama
  **aynı helper'ın diğer iki çağıranında alt katman kontrolü YOK**: `TAPPA_GPS_RADIUS_M=NaN` →
  `config.Load` **err=nil** ve `checkin.New` **err=nil**; `TAPPA_DEBOUNCE_SECONDS=NaN` → sessizce
  60 sn'ye düşüyor. Etki **düşük** (NaN yarıçapta her mesafe karşılaştırması false → GPS-only
  tap'ler `flag`, yani **daraltıcı** yön; §4.6 korunuyor). Tek satırlık `math.IsNaN(v)` reddi üç
  çağıranı birden kapatır — kapsam dışı bırakıldı, `config.go`'da **SINIR** olarak yazılı.
- **M6-11 — *"POST'suz `GET /t` sayısı"* sinyalinin KAYNAĞI YOK.** İmzalı bağlam **stateless**;
  M5-10'un tablosu yapılmadığı için sunucu bir GET'in POST'a dönüşmediğini **bilemiyor**. O kart
  ya kendi kaynağını üretecek ya sinyali düşürecek. Asıl sinyal (`ctr` boşlukları) **çalışıyor**.
- **M5-11 — `sys:tap-freshness` artık CANLI**, yani M5-11'in gün testinde workaround'u kaldırırken
  sayfa yaşına da dikkat: `seedFreshness = 60 sn` (en sıkı yasal pencere) ve ölçüldü ki günün
  sonucu pencere değerine **duyarsız** (900'de de yeşil) — `tapNFC`/`tapQR` GET+POST'u tek çağrıda
  yapıyor, `dayWait` uykuları fazlar **arasında**.

### M5-10 / M5-11 / M6 / M8'e devralınan (M5-09 denetimlerinden)

- **🔴 M8 / M0-06 — CI OLDUĞU GİBİ KIRMIZI VERİR VE BUNU BUGÜNE KADAR KİMSE GÖRMEDİ.**
  Repoda **uzak yok** (`git remote -v` boş — kullanıcı kararı: push/PR yok), yani `ci.yml`
  **hiç çalışmadı**; "CI yeşil" bu projede **teorik** bir cümle. Ve olduğu gibi koşarsa
  kırmızı verir: workflow `DATABASE_URL`/`DATABASE_MIGRATE_URL` veriyor ve `make up`
  koşuyor (yalnız Postgres'i **başlatır**), ama **`make migrate` koşmuyor**, `make seed`
  koşmuyor ve **`TAPPA_TAG_KEK` vermiyor** → `make check` → `make test` DB testlerini
  **migrate edilmemiş** şemaya karşı sürer. Ölçüldü (boş DB'ye karşı `make test`):
  **17 pakette 140 üst düzey FAIL**, bunların **136'sı M5-09'dan eski** — yani bu bir
  gerileme değil, **M1-09'dan beri taşınan** bir durum. Düzeltme tek satır değil:
  `make migrate` + `TAPPA_TAG_KEK` + `make seed` (dördü seed'e bağlı) gerekiyor, ve
  **uzak olmadan doğrulanamaz**. Bu kapanana kadar `make check`'in tek gerçek koşum yeri
  **geliştiricinin makinesi**dir.
- **🔴 M6 — koşum damgası kullanıcıya görünen yüzeye SIZIYOR.** Gün simülasyonu çalışanları
  `Maria Borg [sim 08-02T00:40:24 f4ef]` diye üretiyor; `full_name` → `EmployeeName`
  (`internal/domain/tenant/directory.go:109`) → aktivasyon/tap ekranındaki *"Hello …"*.
  Yani bugünkü dev-DB **ekran görüntüsüne hazır değil** ve M6 dashboard'u da `full_name`
  okuyacak. **Değerlendirilmemiş üçüncü yol** (kartta yazılı): günü `make test`'ten ayırıp
  **yalnız `make simulate-day` içinde seed'li kadroyu** sürmek — hem yeniden koşulabilirliği
  hem demo amacını karşılardı. Damgasız sabit kadro bugün **DB ömrü başına bir kez**
  ağırlanabiliyor (ölçüldü: 2. koşuda tur atlanıyor, `practice=false`, önceki koşumdan
  **açık giriş devralınıyor**) — §4.3 sonucudur, sondanın sınırı değil.
- **🔴 M5-11 — sevk edilmiş §5 ihlali** (practice satırı açık girişi maskeliyor). Kartı
  açıldı, kullanıcı kararı 2026-08-02. Düzeltilince M5-09'un `LIMITS L3`'ü ve kart md. 6'sı
  **kapatılmalı** — yoksa repoda kapanmış bir kusuru açık gösteren iki cümle kalır.
- **M6-07 — `MinutesLate` hem YANLIŞ HESAPLANIYOR hem HİÇBİR YERE YAZILMIYOR.**
  `decide.go` `lateness()` `in.Now`'u (sunucu saati) kullanıyor, kaydın `OccurredAt`'ini
  **değil** → 10:00 vardiyasına 10:17 beyan eden geriye tarihli giriş **`-520`** döndürdü.
  Canlı tap'te ikisi çakıştığı için görünmüyor, ama `occurred_at` sevk edilmiş bir alan ve
  tavanı **72 saat**. Ayrıca `minutes_late` **sütunu yok**, `policy_context`'e yazılmıyor
  (`time:minutesLate` `document.go`'da tanımlı ama `Decide` set etmiyor) → o anahtara
  yazılmış bir tenant politikası **sessizce hiç eşleşmez** (M3-04 invariant'ı). Bu yüzden
  skill'in *"geç kalma"* senaryosu **HTTP'de üretilemiyor** ve M5-09 onu üretmedi.
- **🟡 M6/M7 — `tags.uid` CHECK'i İKİ YAZIMA izin veriyor, zarfın AAD'si ise HAM 7 BAYT**
  (güvenlik denetçisi, ORTA). `hex.DecodeString` büyük/küçük harf duyarsız + `CHECK (uid ~
  '^[0-9A-Fa-f]{14}$')` → `04AC7E55000601` ile `04ac7e55000601` **iki ayrı PK satırı, aynı
  AAD**; ölçüldü: bir plaketin zarfı diğerine **açılıyor**, `last_ctr`'ler **bağımsız**, ve
  çapraz-tenant ikinci satır **INSERT edilebiliyor**. **Bugün sömürülemiyor** — `sun.Parse`
  UID'yi büyük harfe **kanonikleştiriyor** (3/3 yazım aynı satıra düştü) ve `tags`'a INSERT
  eden **hiçbir üretim yolu yok**. Latent: **M8-05** plaket kayıt akışı operatörün yazdığı
  uid'yi olduğu gibi eklerse sonuç **ölü plaket** olur (kaydedilir, hiç tap almaz, hata
  vermez). Çözüm **ikinci temsili silen** tek şey: yeni migration, `CHECK (uid ~
  '^[0-9A-F]{14}$')` (00004 uygulanmış, §6 — yenisi yazılır). Mevcut 12 satır zaten büyük
  harf, veri taşıması yok. **Bu, "kontrol ile tüketici aynı temsili görmeli" sınıfının bu
  oturumdaki DÖRDÜNCÜ vakası.**
- **🟡 M6/M7 — `tappa_app` `tags`'ın DOKUZ sütununda da UPDATE'e sahip** (güvenlik denetçisi,
  ORTA). Ölçüldü: `aes_key_ref`'i bozabiliyor (DoS), `uid`'i yeniden adlandırabiliyor (DoS)
  ve **`last_ctr`'ı 0'a GERİ SARABİLİYOR** (§4.4 replay penceresi). Bugün erişilebilir değil
  (hiçbir üretim sorgusu bu sütunları yazmıyor — `tags` üzerinde tek sqlc sorgusu
  `AdvanceTagCounter`; repoda **dinamik SQL yok**). ⚠️ **Sütun-düzeyi grant TEK BAŞINA
  yetmez:** `last_ctr` listede kalmak **zorunda** (§4.4 advance onu yazar) ve en ağır yetenek
  tam olarak onu geri sarmak. Gerçek çözüm: `REVOKE UPDATE` + `GRANT UPDATE (last_ctr,
  status, retired_at, replaced_by)` **artı** monotonluk trigger'ı (`NEW.last_ctr >
  OLD.last_ctr`).
- **🟡 M8 — `redline-check.sh` kapsamı bir DENETİM İDDİASININ PARÇASIDIR.** `SRC` `test/`'i
  içermiyordu → M5-09'un dört yeni dosyasının **ikisi** hiç taranmıyordu ve *"make audit
  temiz"* göründüğünden az şey kanıtlıyordu. `test` eklendi (ölçüldü: **sıfır** yanlış
  pozitif). Kalan iki sınır: R7 desenleri **tek satırlık**, `fmt.Fprintf(&b,` + duyarlı
  dize sonraki satırda olunca ağdan geçiyor (ölçüldü, `-U` ile yakalanıyor); ve `SRC`'ye
  eklenmeyen her yeni dizin sessizce denetim dışıdır.
- **🟡 M6/M8 — rota başına flood tavanı hâlâ PİNLENMEMİŞ** (M5-07'den devam). M5-09 bunu
  değiştirmedi.
- **⚪ `make help` her hedefi `Makefile` diye yazıyor** — `-include .env` `.env`'i
  `MAKEFILE_LIST`'e sokuyor, grep dosya adı önekliyor. Tek karakterlik düzeltme (`grep -h`),
  M5-09'un işi değildi, commit'e karıştırılmadı.

### M5-09 / M5-10 / M6 / M7 / M8 / M9'a devralınan (M5-08 denetimlerinden)

- **🔴 M8 — DAĞITIM: hiçbir katmanda timeout YOK.** Küme, veritabanı **ve** rol seviyesinde
  `statement_timeout` · `lock_timeout` · `idle_in_transaction_session_timeout` **üçü de 0** (ölçüldü).
  Tek tavan HTTP'deki `middleware.Timeout(30s)`. Advisory kilit geldiğine göre bu artık **kayıp
  penceresinin** de tavanı: `advance` ile `INSERT` arasında 30 sn'ye kadar beklenebilir.
- **🔴 M6-01 / M6-04 — `channel` trust'ın YANINDA gösterilmeli.** `trustScore`'da kanal terimi yok
  (§5 normatif): IP+GPS'li QR **100**, IP+GPS'li NFC **100**. Ayrımı yalnız `transactions.channel`
  taşıyor ve **hiçbir kullanıcı yüzeyinde görünmüyor**. Gösterilmezse müdür `Trust 100`'e bakıp NFC
  sanar. (Kartın *"QR asla NFC trust'ına çıkmaz"* cümlesi bu ölçümle **kanıt tavanı** okumasına
  indirildi: QR aynı kanıtla asla insansız `ok` olamıyor — sayı çakışsa bile.)
- **🔴 M7-03 — politika materyalizasyonu eşzamanlılıkta 23505 alıyor.** `EnsureBaselinePolicy`
  `ON CONFLICT (id)` ile hakemlik yapıyor ama `policies` ayrıca `policies_id_tenant_key (id, tenant_id)`
  taşıyor (00007, FK hedefi) → spekülatif insert **hakem olmayan** indekste yakalanıp düşüyor.
  Gerçek kod yolunda ölçüldü: 40 yarışçı → 3–4/200 bozuk politika seti, log `ensure baseline policy …
  policies_id_tenant_key`. **Fail-safe** (satır yazılır + `flag` + ERROR log, §4.6'nın tarif ettiği
  davranış) ama bakir tenant'ın ilk patlamasında `ok` yerine `flag`. ⚠️ **`ON CONFLICT (id, tenant_id)`
  ÇÖZMÜYOR, kötüleştiriyor** (çarpışmayı `policies_pkey`'e taşıyor, ölçüldü); **PK hakemliği de
  çalışmıyor** (`policy_versions` PK hakemliğinde 9/100). İki unique indeks varken **bilinen çalışan
  bir hakem seçimi yok** — gerçek çözüm 23505 retry / iki indeksi de adlandıran upsert / sign-up'ta
  provisioning.
- **🔴 M9-01 — çevrimdışı kuyruk BU KURALLA KIRILIYOR.** Aynı senkronda saniyeler arayla gönderilen
  iki kuyruklanmış tap'in ikincisi `ignored` olur (ölçüldü: 7 sa arayla `occurred_at`, saniyeler arayla
  POST → `sys:person-debounce`). ADR 0006 bunu **ifşa ediyor, gerekçelendirmiyor** — M9-01 kuyruğu
  tasarlarken kanal/işaretle ayırmak zorunda.
- ~~**M5-10 — tazelik penceresi tavanı düşürüyor…** 3 dk'lık pencere ikinci pencereyi siler, tavanı
  kabaca **yarılar**.~~ **🔴 BU VAAT YANLIŞTI — düzeltildi 2026-08-02 (M5-10 denetimi).** `sys:tap-freshness`
  guardrail'i **NFC-only**'dir ve bu **bilinçlidir** (M3-05; §5: *"QR fotoğraflanır ve süresiz geçerlidir"* —
  QR'da sayfa yaşı diye bir kanıt yok), `TestGuardrails_SunInvalidExemptsQR` o günden beri pinliyor.
  Dolayısıyla **M5-10 QR tavanını DEĞİŞTİRMEDİ**: tek taranmış QR bağlamı bugün de **15 dk TTL** boyunca
  yeniden POST edilebiliyor. Frenler değişmedi — `base:qr-requires-ip` · 60 sn kişi-debounce (fazlaları
  **kayıtlı `ignored`** yapar) · `tapSessionLimit` 300/10dk + `ByAddress` 3000/10dk. Yapısal tavan
  (~600, TTL'e iki pencere) **aynen duruyor**. Sahibi: QR tavanını gerçekten daraltmak isteyen bir görev
  (M6-11 sinyal tarafı / M8 paylaşılan store), M5-10 değil.
- **Kilidin ölçülmüş bedeli (M8 kapasite planı):** bekleyen istek **havuz bağlantısı tutuyor**; tek
  anahtara inen flood'da ilgisiz kişinin gecikmesi **6–9×** (16 bağlantının 15'i `wait_event='advisory'`).
  Tavanlar `ByAddress` 3000/10dk ve 30 sn. **Ölçüm yöntemi de yazılı** (flood **ayrı oturumlardan**,
  kurban **tek atış**) — aksi hâlde `BySession` 300/10dk artefaktı *"fark yok"* dedirtiyor.
- **`SecondsSinceLastRecordedTap` taramayı daraltmıyor.** Pencere yüklemi **sıralamayı** sınırlıyor;
  indeks `(tenant_id, employee_id, occurred_at)`, `created_at` içinde yok → Bitmap Heap Scan kişinin
  tüm geçmişini geziyor (buffer sayısı yüklemli/yüklemsiz **birebir aynı**). §4.3 satırları kalıcı
  kıldığı için taranan küme **yalnız büyüyor**, en hızlı **flood edilen kişide**. `created_at` indeksi
  = migration, alınmadı.

### M5-09 / M6 / M8'e devralınan (M5-07 denetimlerinden)

- **Rota başına flood tavanı PİNLENMEMİŞ.** Beş `flooded` çağrısından yalnız `activate_submit` bir
  testle tutuluyor (`TestBudgets_FloodCeilingStillRefuses`); `activate_tour` **ve** `activate_done`
  gate'i silinince suite **yeşil** kalıyor (ölçüldü). M5-07 mevcut boşluğa **beşinci üye ekledi,
  boşluğu açmadı**. Rota başına *"floodLimit+1. istek 429"* tablo testi **kendi görevini hak ediyor**.
  ⚠️ **2026-08-03: bu satır AKTİVASYON ailesi için hâlâ doğru** (`activate.go`'da hâlâ tam 5 çağrı, hâlâ
  yalnız `activate_submit` tutuluyor) **ama artık eksik konuşuyor:** M6-01 B panelin **beş** flood çağrısını
  bir tablo testiyle **pinledi** (`TestAdminAuth_FloodCeilingRefusesEveryUnauthenticatedRoute`, kapsanan
  rota **5**), yani boşluk **yarıya indi**. Kalan borç: `activate_tour` · `activate_done` — ve o üç çalışan
  rotası **altyapı** istiyor (Tap/Activation koşum takımı: DB, davet yöneticisi, oturum yöneticisi, SUN
  doğrulayıcı), M6-01'in kapsamı değildi.
- **Dokunma hedefi ölçümü markup okur.** `TestTour_HasExactlyTheseTouchTargets` HTML'e bakıyor;
  **salt CSS ile** basılabilir hale getirilmiş bir alanı (dev `::after`) hiçbir test göremez. Kapalı
  küme içinde basılabilir olan yalnız `<a href>` (`button`/`form`/`input`/`area`/`label`/`details`
  kümenin dışında), o yüzden bugün dar; ama piksel ölçen test yok.
- **Turun *"never counts toward your hours"* cümlesi GELECEK ZAMANLI.** Repoda saat toplama **yok**
  (0 eşleşme); sahibi **M6-07** ve o kart `practice = true` kayıtların saate dahil olmadığını zaten
  yazıyor. Bugün sınanabilen kısım yalnız **yön zinciri**.
- **Tur `Activation.render`'dan geçiyor → CSP yok** (aşağı md.3, dokuz ekran). Güvenlik denetçisi §4
  açısından kabul edilebilir buldu: turda form yok, script yok, `<a>` dışında basılabilir eleman yok.

### M5-08 / M6'ya devralınan (M5-06 denetimlerinden)

1. **`checkin.go:205` outcome switch'inin `default:` dalı Result ("recorded") ekranını render ediyor.**
   Bugün yalnız `OutcomeRecorded` oraya düşüyor, ama ileride *"yazılmadı"* anlamına gelen bir outcome
   eklenirse **kaydın var olduğunu söyleyen ekranı miras alır** (§4.6). Doğrusu `case OutcomeRecorded:`
   + gürültülü default. **M5-05'ten geliyor**, M5-06 dokunmadı (kapsam).
2. **`sys:tenant-mismatch` dalı `audit_log` satırı YAZMIYOR** (`checkin.go:560-563`, yalnız `slog.Warn`;
   `ActionTapUnknownTag` var, bunun karşılığı yok). Güvenlik denetçisi: bir tenant'ın oturumunun başka
   tenant'ın plaketine dokunması **anlamlı bir olay** ve DB'de izi kalmıyor → §4.5 kanıtı **log
   rotasyonuna bağlı**. §4.6 ihlali değil (o yolda yazılacak meşru mesai kaydı yok). **M6** (dashboard
   anomali sinyalleri) ya da daha erken.
3. **DOKUZ aktivasyon ekranında CSP yok** (M5-07 denetiminde yeniden sayıldı: `Activation.render`
   `Content-Security-Policy`'yi **0 kez** set ediyor → `Activate` · `Confirm` · `Done` · **`Tour`** +
   beş `problem*` sabiti). ⚠️ **2026-08-03 güncellemesi: artık İKİ** `Header().Set("Content-Security-Policy")` var
   (`tap.go:602` + `adminlogin.go:978` — M6-01 B panelin **gövdeli her yanıtına** CSP koyuyor); aşağıdaki
   "tek" ifadesi M5-07 dönemine aitti. Aktivasyon ailesinin **dokuzu** hâlâ korumasız. Eski hâliyle: `internal/handler`'da tek `Header().Set("Content-Security-Policy")` var
   (`tap.go:602`) → **tap ailesinin altısı** korunuyor, aktivasyon ailesinin **dokuzu** korunmuyor;
   ortak `pages.Problem` şablonu ikisine birden hizmet ettiği için asimetri aynı şablonun içinden
   geçiyor. Aktivasyon `Message` dizeleri de o pakette pinli değil. Üç denetçi "düşük riskli, asimetriyi
   kaldırır" dedi ama **M5-02'nin akışı** olduğu için dokunulmadı. *(M5-06'da bu satır "beş" diyordu;
   M5-07 `Tour`'u ekleyip sayıyı 8'den 9'a çıkardı ve doğru sayım o denetimde yapıldı.)*
4. **CI `make css` koşmuyor** (`ci.yml`: yalnız `make tools`/`up`/`check`/`audit`; `app.css` gitignore'da)
   → derlenen CSS'i okuyan **iki test de** (`TestCompiledCSS_GeneratesNoText` ve `TestCompiledCSS_StampWordIsInk`)
   **CI'da daima SKIP**, ve bir skip **pass değildir**. Hem CSS ile üretilen metin kanalı hem damga
   renginin ink kaldığı yalnız geliştiricinin makinesinde, elle `make css` sonrası korunuyor.
   **M8** (ya da tek satırlık CI adımı).
5. ~~**Damga kontrastı — kullanıcı kararı bekliyor.**~~ **KAPANDI — kullanıcı kararı 2026-08-01:**
   *damga METNİ `ink`, durum RENGİ çerçevede.* Eşleme korundu, palete yeni token girmedi. Ölçüldü
   (`paper #FFFDF4` üstünde, damganın `.docket` içinde olduğu render edilen HTML'den doğrulandı):
   **ÖNCE** approved 7.73 / flagged **2.62** / rejected 5.30 / ignored **1.52** / training 16.17, ve
   `.stamp`'in `opacity:.8`'iyle efektif 4.70 / **2.14** / **3.77** / **1.39** / 8.54 → **beşten üçü AA'nın
   altındaydı**. **SONRA** 13.85 / 14.81 / 13.99 / 15.55 / 13.27 — hepsi geçiyor. `opacity-80` **kaldırıldı**
   (AA için değil: çerçeve artık tek renk taşıyıcı ve grup opaklığı onu da soluklaştırıyordu).
   **Kalan sınır (yazılı, kabul edildi):** çerçevenin kendisi WCAG 1.4.11'in **metin-dışı 3:1** eşiğini
   `saffron` (2.62) ve `line` (1.52) için geçmiyor — durumu **kelime** taşıdığı için kabul edildi.
6. **Ekran-başına elle kapsam.** M5-06'nın üç beyaz listesi (metin · eleman · referans) **ekran başına**
   ve **elle**: bu pakete sonradan eklenen bir şablon, biri onu **iki listeye de** yazana kadar hiçbiriyle
   kapsanmıyor. Hiçbir şey bunu zorlamıyor — yazılı sınır.

### M5-08 / M5-10 / M6'ya devralınan (M5-05 denetimlerinden)

1. **🔴 `tap:trust` · `tap:direction` · `tap:practice` · `time:minutesLate` policy context'inde YOK**
   (Evaluate **sonrası** hesaplanıyor — M4'ten miras, kötüleştirilmedi). Bunlara yazılmış bir **tenant
   politikası sessizce hiç eşleşmez**. **M6-09** (policy yönetim ekranı) politika yazma yüzeyini
   açmadan **önce** ele alınmalı — yoksa müşteri çalışmayan kural yazar ve bunu fark etmez.
2. **Sayaç kayıttan ÖNCE harcanıyor.** `advance` ile `insert` arasındaki **her** hata (altyapı hatası
   **ve** istemci bağlantı kesme) o basışın kanıtını götürür: ölçüldü, `transactions` **+0**,
   `last_ctr` **700→701**. Kalıcı kayıp değil (yeni dokunuş yeni `ctr` üretir), **sessiz de değil**
   (ERROR log + "Nothing was recorded … Tap the plaque again"). Bugün dar (`tappa_app` `tags`/
   `employees` silemez). Yazma'yı istekten koparmak **daha kötü** olurdu (terk edilmiş istek mesai
   kaydeder).
3. **`transactions` artık bir YAZMA BÜTÇESİ.** 40 POST → **40 silinemez satır**; bütçeler 300/10dk
   (oturum) + 3000/10dk (adres) ⇒ ~43k/oturum/gün, ~432k/mekân-adresi/gün. Atfedilebilir olduğu için
   sahtecilik değil **gürültü/depolama** — ama M5-02'nin *"koruma maliyeti saldırı olmasın"* argümanı
   **yalnız `audit_log` için** kurulmuştu. **M8** (paylaşılan store + mekân-başına anahtarlama).
4. **`acc` (GPS doğruluğu) kimse tarafından okunmuyor** — sütun yok, kural yok; **5 km'lik bir fix
   sıkı olanla aynı sayılıyor**. Trust puanı sahte-GPS'e karşı bunu kullanabilirdi (ADR 0005 A3).
5. **Debounce temeli herhangi bir verdict** — 10 sn önceki bir `reject` gerçek bir tap'i yutabilir.
6. **QR bağlamı TTL boyunca tekrar POST edilebilir** (ilerletilecek sayaç yok) → **M5-08**'in işi.
7. **Oturumsuz POST tag'e atfedilemiyor** (bağlam oturum id'si üzerinden MAC'li) → 00005'in çalışansız
   reject satırı bugün **erişilemez**.
8. ~~**Damga sınıfları:** literaller artık `.templ`'de; `app.css` gitignore'da…~~ **KAPANDI (M5-06).**
   Taze build ile doğrulandı: beş damga sınıfı derlenen CSS'te (`approved` 1 · `flagged` 2 · `rejected` 2 ·
   `ignored` 2 · `training` 2, `grep -o | wc -l`), renkler palete birebir, negatif kontroller 0. Ayrıca
   **kural genelleşti:** CSS sınıf adı üreten Go kodu **sıfır** — üretim kodunda sınıf adı yalnız taranan
   `.templ` dosyalarında literal olarak yaşıyor.

### M5-05'e devralınan (M5-04 denetimlerinden)

1. **🔴 `sun.Verify` POST'ta ÇAĞRILAMAZ.** İmzalı bağlam CMAC **taşımıyor** (kart tuzağı gereği) →
   CMAC'siz `Params` ile `Verify` çağrılırsa `verifyMAC` **false** döner, `SUNValid=false`, sayaç
   **hiç ilerlemez** (fail-closed ama her NFC tap flag'e düşer). Sözleşme:
   **`sunValid == ctx.CMACVerified && AdvanceCounter başarılı`**. `AdvanceCounter` zaten exported ve
   `WithTenant` ister. Kart bu satırda düzeltildi.
2. **Tag'i POST'ta YENİDEN ÇÖZÜMLE** — durum GET ile POST arasında değişebilir (§5 satır 1:
   `lost`/`retired` → `reject`). `Preview` artık `TagStatus` taşıyor ama o **GET anının** durumu.
3. **🔴 QR bağlamı TTL boyunca tekrar POST edilebilir.** NFC'de atomik ilerletme durdurur;
   **QR'da ilerletilecek sayaç yok** → tek savunma **60 sn debounce** (person-scoped, guardrail'de).
   M5-08 QR kanalını ele alırken bunu bilmeli.
4. **Yabancı-tenant `ctx`'i** base64 çözülünce o tenant'ın **UUID'lerini** açıyor (ad değil, opak
   kimlik; plakete fiziksel dokunmuş birine). Denetçi §4.5 ihlali saymadı — sertleştirmek isteyen
   şifreleme ekleyebilir.
5. **Seed `aes_key_ref` KEK-sarmalı DEĞİL** → seed'li plaketler NFC yolunda **500** veriyor
   (`unwrap: wrapped ref must be 44 bytes, got 42`). M5-05'i **bloklamıyor** (birim/DB testleri kendi
   plaketlerini üretiyor) ama **M5-09'u ("bir günü simüle et") HTTP üzerinden NFC ile BLOKLAR**.
   Çözüm: seed `sun.Wrap(kek, uid, key)` ile **yapısal olarak doğru** sahte anahtar üretsin (§4.7:
   gerçek anahtar repoda yer almaz). Backlog **T7** ile aynı kök.
6. **CSP yalnız tap yanıtlarında** — aktivasyon ekranları (M5-02) bilinçli olarak dokunulmadı
   (o akış dört denetim turu gördü, kendi görevini hak ediyor).

### M5-04 / M5-05'e devralınan (M5-03 denetimlerinden)

1. **🔴 `TapLimiter`'ı MONTE EDECEK OLAN M5-04/M5-05'tir** ve montaj **sırası bir sözleşmedir**:
   `ByAddress` (DB işinden **önce**) → `Identify` → `BySession`. Sıra bozulursa `BySession`
   `SessionUnresolved` görür ve **500** verir (bilerek gürültülü: ölçülmeyen istek sessizce geçmesin).
2. **🔴 429'un §4.6 kalıntısı — adıyla yazılı, çözülmedi.** Bir mekânın paylaşılan adres bütçesini
   düşmanca bir cihaz harcarsa o mekânın **meşru tap'leri 429 alır** ve istek **karar motoruna hiç
   ulaşmaz** → ne `transactions` satırı ne `flag`. §4.6 tam da bunu `flag` ile karşılamak için var.
   Sınırlandı, çözülmedi (M8: paylaşılan store + mekân-başına anahtarlama).
3. **`Identity` sıfır değerinde `Err == nil` VE `!Live()`** → `if id.Err != nil {500} else if !id.Live()
   {aktivasyon}` yazan bir handler tam **§5 satır 3**'e düşer. `identity.go` dikkatli yazılmış ("artık
   olamaz" demiyor), ama M5-04/M5-05 bu ayrımı **açıkça** ele almalı.
4. **§5 satır 3 yönlendirmesi M5-04'ün işi.** M5-02 hedefi (aktivasyon sayfası) kurdu ve `transactions`
   yazmıyor; oturumsuz tap'in oraya yönlendirilmesi bağlanmadı. ⚠️ M5-02'nin **koşullu** çerez-ekim
   savunmasıyla birleşiyor: `GET /t` oturumsuz tap'i `/activate`'e yönlendirdiğinde, ekili bir davet
   çerezi olan tarayıcı **yabancı tenant'ın formunu** görebilir (form artık hangi işletme/çalışan
   olduğunu butonun üstünde yazıyor — tek gerçek engel bu).
5. **Fontlar self-host DEĞİL** (`web/static/fonts/` yok, `@font-face`=0, sistem fontuna düşüyor,
   **dış istek yok**) → M5-04 kabul kriteri.
6. **429 gövdesi düz metin** — markalı sayfa `tappa-brand` ile M5-04'te.
7. **Limiter süreç-içi ve sabit-pencere**; iki instance sınırı ikiye katlar, pencere sınırında 2×limit
   mümkün. `limiterMaxKeys` 100k aşılınca map **toptan sıfırlanıyor** (fail-open, bilinçli ve yazılı).
   Hepsi M8 (paylaşılan store).
8. **`TAPPA_TRUSTED_PROXIES` kalan sınırı:** kapı yalnız **tek girdilik** `/0`'ı yakalar; `/1` ya da
   birleşimle tüm uzayı kaplayan liste (`0.0.0.0/1,128.0.0.0/1`) **yakalanmaz** — yazılı. Ayrıca
   güvenilen aralık **gerçek istemcileri içeriyorsa** onlar serbestçe uydurabilir (ölçüldü). M8 dağıtım
   kontrol listesi. Proxy'nin XFF'e **append** etmesi zorunlu; **replace** eden proxy buradan
   ayırt edilemez.

### M5-03'e devralınan (M5-02 B denetimlerinden)

1. **🔴 ADLANDIRILMIŞ YÜKÜMLÜLÜK — gerçek istemci IP.** `internal/handler` `clientIP` bilinçli olarak
   **`X-Forwarded-For` okumuyor** (M5-03'ün işi). Sonucu ölçüldü: ters proxy arkasında **her istek tek
   anahtarı paylaşır** → flood tavanı (600/10dk) **küresel**, yani dışarıdan tek bir çağıran tavanı
   harcayıp o pencerede **herkesi** bloklayabilir; `unknown` bütçesinin log bastırması da küresel.
   M5-02'de **ucuz** hâli kapatıldı (60 bilinmeyen kod artık geçerli bir aktivasyonu reddetmiyor — üç
   ayrı bütçe), ama kalanı bir sayı ayarı **değil**. M5-03 gerçek IP'yi çözmeli **ve `floodLimit`'i
   yeniden değerlendirmeli**. Kartın "gerçek IP yalnız `cfg.TrustedProxies` hop'larından" kriteri
   birebir bu iştir; chi `middleware.RealIP` başlığa **koşulsuz** güvendiği için kullanılmaz (R5).
2. **Oran sınırlayıcı süreç-içi** — iki instance sınırı ikiye katlar; sabit-pencere (fixed window)
   sınırında kısa sürede 2×limit mümkün. İkisi de `ratelimit.go`'da sınır olarak yazılı → paylaşılan
   store M8'de.
3. **Aktivasyon çerezi ekimi oturumsuz telefonda durdurulmuyor** (önlem 3 koşullu: yalnız *çakışan
   oturum + çapraz-site* dalında ateşler). Bugün ek yetenek vermiyor (saldırgan aynı linki doğrudan da
   gönderebilir — ADR 0005 Y-D kimlik avı). **Ama M5-04 devir riski:** `GET /t` oturumsuz tap'i
   `/activate`'e yönlendirecek, yani ekili çerezi render eden sayfaya. Form artık **hangi işletme +
   hangi çalışan** için aktive olunduğunu butonun hemen üstünde yazıyor; tek gerçek engel bu.
   `Sec-Fetch-Site` **yoksa same-site sayılıyor** (fail-open, yazılı).
4. **Çerez gölgeleme:** aynı isimli iki `tappa_activation` çerezinde `r.Cookie` **ilkini** alıyor
   (alt alan adı kontrolü gerektirir). `__Host-` öneki M8'e yazılı.
5. **`ConfirmView.Code` düz `string` kalmak ZORUNDA** — templ bir değeri render ederken string biçimini
   ister, redakte eden tip `invite.Code(redacted)` **post ederdi**. Beyan edilmiş tek istisna, tek
   görünüm, tek yol; koruma tip sistemi değil **inceleme**. Yeni bir görünüme kod alanı eklenirse bu
   muhakeme tekrarlanmalı.
6. **§5 satır 3 BAĞLANMADI** — M5-02 hedefi (aktivasyon sayfası) teslim etti ve **`transactions` satırı
   yazmıyor**; oturumsuz tap'in oraya yönlendirilmesi **M5-04**. Fontların self-host edilmesi de M5-04
   (bugün `web/static/fonts/` yok, `@font-face` = 0, sistem fontuna düşüyor, **dış istek yok**).
7. **Davet üreten HTTP uç noktası YOK** (bilinçli): admin auth **M6-01**. Kimliksiz bir uç nokta tam da
   Y-D riskini genişletirdi. Q02 çözülene kadar gönderim **arayüz ardında**; kod çalışanın kendi
   kanalına gidince Y-D daralır (ADR 0005).

### M5-02 / M5-03'e devralınan (M5-01 denetimlerinden, bloklamayan)

1. **🔑 Deaktif çalışanın çerezi CANLI oturum olarak çözülmeye devam eder** (3. tur denetçisi, en önemlisi).
   Bilinçli: `resolve_session_by_token_hash` `employees.status` döndürmez ve deaktivasyon oturumları
   **iptal etmemelidir** (aşağı md. 2). Sonuç: **tap yolu dışındaki** her kimlik doğrulayan yüzey
   (M5-03 middleware, ileride herhangi bir çalışan sayfası) `employees.status`'ü **kendisi** kontrol
   etmek zorunda. Tap yolunda otorite `sys:employee-deactivated` guardrail'idir; başka yerde otorite yok.
2. **Deaktivasyon (M6-05) `RevokeAllForEmployee`'yi ÇAĞIRMAZ.** Denetçi kanıtladı: `decide.go:96`
   `CtxEmployeeStatus`'ü doğrudan `Employee.Status`'ten kurar ve `sys:employee-deactivated` yalnız ona
   bakar → iptal reddetmeye hiçbir şey **katmaz**, yalnız §4.6 kayıp koşulunu üretir (guardrail sırası:
   `sys:no-session` **#6**, `sys:employee-deactivated` **#7** → iptali "oturum yok"a çeviren çağıran
   önce redirect alır, **kayıt yazılmaz**). Meşru çağıranlar: çalınan/kayıp telefon, M5-02 ikinci aktivasyon.
3. **`Verify` API tuzağı — `if err != nil { aktivasyon }` YANLIŞ.** `ErrNoSession` için doğru,
   **`ErrRevoked` için değil**: `Verify` iptalde **dolu `Resolved`** döndürür (çağıran §5 satır 4'ü
   uygulayıp kaydı yazabilsin). Sözleşme `manager.go`'da 3 adımda yazılı. Tip zorlaması **bilinçli
   yapılmadı**: `(Resolved, Outcome, error)` şekli çağıran Outcome'u kaçırırsa iptal edilmiş çerezi
   CANLI sayar = **fail-open auth bypass**; bugünkü en kötü hâl fail-closed'dır.
4. **`sessions.revoked_at` UPDATE ile NULL'a çekilebilir** (kapanış denetçisi, canlı ölçüm: `tappa_app`
   olarak `UPDATE sessions SET revoked_at=NULL` → 1 satır). `tappa_app`'in tablo geneli UPDATE'i
   `last_used_at` için **gerekli**, sütun-düzeyi grant bu ayrımı ifade edemez. Gönderilen 5 sorgunun
   hiçbiri yapmıyor (`COALESCE` yalnız ileri yönlü) ve kural `db/queries/sessions.sql`'de yazılı:
   `revoked_at` NULL → non-NULL, asla geri. **Yapısal fix bir trigger'dır ve YENİ migration ister**
   (00003 immutable, §6) — M6/M7'de değerlendirilir. Şu an koruma dosya disiplininde, DB'de değil.
5. **`config.Load` `BaseURL`'ü doğrulamıyor** (2. kapsam genişlemesinden bilinçli kaçınıldı). `NewCookies`
   **prefix testi** yapar, URL parse etmez → başında boşluk olan veya URL olmayan `BaseURL` NOT-Secure
   dalına düşer (**non-prod'la sınırlı**; prod koşulsuz Secure). M5-03/M8 config sertleştirmesinde ele alınır.
6. **`TAPPA_ENV` YOKLUĞU hâlâ sessizce `dev`** — enum yalnız *yanlış* değeri reddeder, *eksik* olanı değil
   (kasıtlı varsayılan). TLS sonlandıran proxy arkasındaki prod'da operatör `TAPPA_ENV`'i unutur ve
   `TAPPA_BASE_URL`'i iç http adresinde bırakırsa çerez Secure'suz gider. Bugün sömürülebilir değil
   (`NewCookies`'in test dışı çağıranı yok). Kalan savunma **operasyoneldir** → M8 deploy denetimi.
7. **`DeviceInfo` sınırı UA'yı ENGELLEMEZ, yalnız sütunu sınırlar.** Bilinçli olarak **kısa** bir user
   agent bu sınırdan geçer. M5-02 `r.UserAgent()`'ı **doğrudan geçirmemeli** (§7: dış girdi handler
   sınırında doğrulanır) — kaba etiket türetmeli.

### M2-04'e devralınan not (M2-01 denetiminden, N3)
SV2 içindeki `ctr`'nin byte sırası ADR/skill'de açıkça sabitlenmedi (bilinçli) → **M2-04/M2-07
bilinen-cevap vektörleriyle** sabitlenmeli (little vs big-endian sessizce yanlış "makul" değer üretir).

### M6'ya devredilen (M1-11 denetiminden) — back-FK boşluğu
`transactions.entered_by` · `transaction_reviews.reviewer_id` · `audit_log.actor_id` →
`admin_users` FK'leri **eklenmedi** (00005 yorumları "M1-11'de eklenir" demişti — **artık
yanıltıcı**, 00005 immutable/düzeltilemez). M1-11 kartı bunları istemiyor + `reviewer_id NOT NULL`
FK'si rls_test fixture'ını (rastgele reviewer_id) kırardı. ⚠️ **2026-08-03: M6-01 BUNU YAPMADI ve
yapamazdı** — iki fazı da **yeni migration olmadan** sevk edildi (00011 A fazınındır ve `admin_users`'a
back-FK eklemez; B fazının `db/` diff'i **boş**). **Kalan tek sahip M6-04.** Eski hâliyle: **M6-04 (review akışı) / M6-01 (auth)**
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
- ⚠️ **2026-08-03: bu satırın `admin_sessions` yarısı ARTIK YANLIŞ.** 00011 (M6-01 A, `66d5442`)
  `admin_sessions_revocation_monotonic` trigger'ını **sevk etti** — `revoked_at` monoton, `tappa_owner`
  bile geri alamıyor — ve tablo-geneli UPDATE'i **REVOKE** edip yalnız `(last_used_at, revoked_at)`
  sütunlarına GRANT verdi. Ayrıca **`admin_sessions`'da `expires_at` sütunu HİÇ YOK** (kolonlar: id,
  tenant_id, admin_user_id, token_hash, created_at, last_used_at, revoked_at) — o alan
  `password_resets`'e ait. **Sağ kalan doğru yarı:** `password_resets.used_at`/`expires_at` gerçekten
  hâlâ serbest. ⚠️ Ve **daha güçlü kill switch'in hiçbir monotonluk koruması yok**: `admin_users.status`
  tek satırla o admin'in **tüm** oturumlarını öldürüyor, ama `SET status='active'` hepsini geri getiriyor
  (ölçüldü). Eski hâliyle: `password_resets.used_at`/`admin_sessions.revoked_at`/`expires_at` UPDATE-edilebilir (append-only
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

### ⏳ Bekleyen kullanıcı eylemleri → **[docs/backlog.md](../backlog.md)**

Kullanıcının yapabileceği (ajanın kodla kapatamayacağı) işler artık **tek yerde**:
[docs/backlog.md](../backlog.md). Buraya kopyalama — çelişirler.

- **B1 — iOS Safari çerez ömrü ölçümü (Q11).** Gerçek iPhone ister. **Hiçbir şeyi
  bloklamıyor:** M5-01 sunucu tarafı ölçümden bağımsız, bu yüzden kabul kriteri olamadı
  (kart düzeltmesi 2026-07-31). Sonuç `open-questions.md` → Q11'e yazılır.
- **B2 — arm64 Go kurulumu (Q26).** `sudo` ister. **Hiçbir şeyi bloklamıyor** — her şey
  amd64 Go 1.26.5 ile yeşil; kazanç yalnız yerel derleme/test hızı.

**Kullanıcı "backlog ekle" derse madde oraya yazılır.**

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
| `git status --short` | temiz (görev arasındaysan) |
| `ls .env` | var (git'e **girmez**) |
| `docker compose ps` | `tappa-db` ayakta ve `healthy` |
| `make migrate-status` | **00001–00022 uygulanmış** (2026-08-24). 🔴 **00022 = M8-05 B2c-2a** — `tags.encoded_at` + **iki trigger** (yaz-bir-kez · **INSERT'te kurulamaz**, satır **damgasız doğar**) **ve `REVOKE SELECT ON tags` + dokuz sütunluk `GRANT`** → **`aes_key_ref` on sütunun tek okunamayanı** (**T16'nın SELECT yarısı**). `Down` **taze klonda** koşuldu, `498 vs 498` **IDENTICAL**. ⚠️ **`tappa_app` artık `tags`'i tablo düzeyinde SELECT edemez** (`relacl = tappa_app=a`); zarfı okumak isteyen bir test **`tappa_owner`'a** geçmeli (üç test öyle taşındı). *(Aşağıdaki 00020 açıklaması tarihsel ve geçerli.)* **00020 = M7-06** `legal_documents` — **`tenant_id` TAŞIMAYAN tek tablo**, §6'ya gerekçeli istisna (ADR **0016**), `redline-check.sh` muafiyet sözdiziminin **repodaki ilk kullanımı** → **her koşuda R5 WARN, bu beklenen**. **00019 = M7-04 A** `password_resets` sertleştirmesi (ADR **0015**) — **00006'dan beri açık olan aynı-tenant yetki yükseltmesini** kapattı. **00018 = M7-03 A** `password_hash` işlenebilir-bcrypt CHECK'i (ADR **0014**), `NOT VALID`. **00017 = M7-02** (`vat_verified` + `resolve_admin_by_email` ilk-gelen sırası; ⚠️ `admin_users`'ta `created_at` INSERT/UPDATE grant'ının **dışında** ve bu o migration'ın tüm argümanının dayanağı — **açma**). **00016 = M6-12 A** (billing). ⚠️ **Aşağıdaki 00013/00012 açıklaması tarihsel, doğru ve hâlâ geçerli.** **00013 = M6-06 B veri katmanı** (`tags` sertleştirmesi + envanter): `uid` CHECK **NOT VALID** (gerekçe: 18.010 küçük harfli satırın 12.437'si transaction'dan referanslı → §4.3), `REVOKE UPDATE` + **beş sütun** GRANT + monotonluk trigger'ı, `location_id` **nullable** + `unassigned` + üç CHECK, T11 indeksi, ad CHECK'leri. `Down` **taze klonda koşuldu**, fresh v12 ile **IDENTICAL** — ⚠️ ama **bir güvenlik geriye-gidişidir** (dokuz sütunda UPDATE geri geliyor, trigger düşüyor). **00012 = M6-05 B** (`employee_invites.cancelled_at`) — projenin **ilk güvenlik düzeltmesi migration'ı**; sütun düzeyi `GRANT UPDATE (used_at, cancelled_at)`, `Down` **gerçek** (ayrı DB'de 12→11 koşuldu), ve `resolve_invite_by_code_hash` **DROP+CREATE** edilirken `OWNER`/`REVOKE`/`GRANT` **yeniden kuruldu** (DROP üçünü de sessizce atar → ADR 0002 §7 bypass'ı) |
| `make audit` | ✅ **exit 0** (2026-08-20, `go1.26.7`): `govulncheck exit=0` + `redline-check exit=0`. 🔴 **BU SATIR 2026-08-20'YE KADAR *"BUGÜN exit 2 VE BU BEKLENEN"* DİYORDU ve o gün `:7`'deki *"T31 KAPANDI, exit 0"* ile ÇELİŞİYORDU** — yani her oturumun **ilk koştuğu tablo** kendi dosyasının başlığını yanlışlıyordu ve yeni bir ajan `exit 2` bekleyip `exit 0` görünce (ya da tersi) yanlış tarafa yorardı. İki bağımsız denetçi bunu M8-05 FAZ B1'de bulmak zorunda kaldı. *(Tarihsel: sebep **govulncheck**'ti — 6 stdlib açığı, `go1.26.5 → 1.26.6` ile kapandı; **T31 kullanıcı tarafından kapatıldı**.)* ⚠️ **Bu satırı "kırmızı, demek ki bozuk" diye okuma** — ve *"zaten kırmızı"* alışkanlığına da düşme: M7-03 B, govulncheck exit verince make'in durduğunu ve **`redline-check.sh`'in sekiz gündür hiç koşmadığını** ölçtü. Artık iki tarama **bağımsız** koşuyor ve **ikisinin de çıkışı** son satırda raporlanıyor (`audit: govulncheck exit=N - redline-check exit=M`). **`./scripts/redline-check.sh` tek başına exit 0 olmalı** — ve `rg` **yok/bozuk/çalıştırılamaz ya da taraması patlıyorsa exit 2** verir (*"atlandı ≠ temiz"*), **1**'den ayrı (o *"ihlal bulundu"*) |
| `make check` | **exit 0** — ama yalnız **temiz ağaçta** (aşağı). ⚠️ **2026-08-07'den beri `gen` de koşuyor** (`check: fmt gen lint test`, kullanıcı kararı) → **+10–15 sn**; bayat `_templ.go`/`*.sql.go` artık **CI'da kırmızı**. **Gözlenen aralık 244–300 sn, yüke bağlı** — `2e7ec64`'te 244 sn, `92e0e23`'te (load 2,90→3,73) ~300 sn, `145b344`'te (load 1,87→2,75) **246 sn**. ⚠️ **Ölçerken repoya DOKUNMA:** son adımı `git diff --exit-code` olduğu için, koşu sırasında bir doküman düzenlemek **exit 2** verdirir ve bu *"testler kırmızı"* diye okunur (2026-08-14'te bir kez oldu; 20 paketin 20'si `ok`'ken çıkış 2'ydi) |
| `make test` | **23 paket** `ok` (2026-08-21, `6bb6cdf` — 22 → **23**, yeni: **`internal/encode`**; `internal/handler` 241,9 sn ve `internal/adminauth` 108,5 sn baskın, load ~2,9). *(Aşağıdaki 22/3507 satırı M8-01 dönemine ait, tarihsel.)* **22 paket** `ok`, **PASS 3507 / SKIP 0 / FAIL 0** (**M8-01 kapanışı, 2026-08-15, `4ddd11f`**; paket sayısı 20 → **22**, yeni: `internal/buildinfo` ve testli `cmd/tappa`) · *(3469/20 satırı M7-05 dönemine ait, tarihsel)* · ⚠️ **çıplak `go test` bu repoda 450 SKIP** — ve bu oturumda **iki denetçi** o tuzağa düştü, biri **mutasyon altında `ok` aldı** çünkü dört DB testi sessizce SKIP oldu; *"N test geçti"* diyen her iddia **hangi komutla** ölçüldüğünü söylemeli · *(aşağıdaki 3389 satırı M7-04 B dönemine ait, tarihsel)* · *(aşağıdaki 3345 satırı M7-06 dönemine ait, tarihsel)* · **gözlenen aralık 234–565 sn ve TAMAMEN YÜKE BAĞLI** — aynı suite bu oturumda `load 3` altında **234 s**, `load 155` altında **565 s** koştu; ⚠️ `internal/handler` tek başına ~256–300 s ve bu **bilinçli, kabul edilmiş bir sınır — yeniden açma**. **Her süreye `uptime` yaz.** *(aşağıdaki 92–180 sn bandı M6-06 dönemine ait, tarihsel)* · **gözlenen aralık 92–180 sn** (makine durumuna göre; **hedef değil, gözlem kaydı**) · sayım: `make test GOFLAGS=-v \| grep -c -- '--- PASS:'` · ⚠️ **bu sayı her görevde artar — bayatlarsa güncelle, canlı iddia sanma** · ⚠️ çıplak `go test` DB testlerini **sessizce SKIP eder** (M6-06 A'da bir denetçi `.env`'siz koşup **276 SKIP** aldı; `make` `.env`'i `-include`+`export` ile yüklüyor) |
| `make test-short` | **gözlenen aralık 51–74 sn**, **TAM 3 SKIP** (`TestAuthenticate_TimingIsFlat`, `TestPanelE2E_TimingIsFlatOverHTTP`, `TestSeedDB_ADayAtKFStJulians`) — iç döngü içindir, **commit öncesi `make test`**. ⚠️ Bu bant **üç kez dar yazılıp üç kez tutmadı**; artık gözlenen aralık ve **hedef değil** |
| 🔴 **`make check` KIRMIZIYSA ÖNCE BUNA BAK — bu çalışma modunun tuzağı** | **Terk edilmiş bir ajan sondası, suit'i kırmızı yapar ve hata mesajı YANLIŞ SEBEBİ gösterir.** Ölçüldü 2026-08-20 (M8-05 FAZ B1): sekiz denetçi gün boyu `BEGIN … ROLLBACK` sondaları koşturdu; **ikisi istemci tarafında vazgeçti ama sunucu tarafında sorgu koşmaya devam etti** (`pg_stat_activity` → `state='active'`, **1s 16dk** ve **36dk**). Salt-okuma sorguları `ACCESS SHARE` tutuyor, `TRUNCATE`'in istediği `ACCESS EXCLUSIVE` ile **çakışıyor** → `TestAppendOnly_TruncateIsRefusedEvenForTheOwner` (ve kardeşi `TestAppendOnly_TruncateCascadeIsRefusedToo`) **kırk denemede** kilidi alamıyor ve `internal/db` düşüyor. ⚠️ **Testin mesajı doğru ama DAR:** *"LOCK CONTENTION with **concurrent packages**"* diyor — gerçek sebep eşzamanlı paket değil **terk edilmiş bir istemci**ydi; test **tek başına** koşarken bile düşüyordu. **Teşhis:** `SELECT pid, state, now()-state_change, left(query,60) FROM pg_stat_activity WHERE state <> 'idle';` · **çare:** o `pid`'leri `pg_terminate_backend` ile düşür (salt-okuma sorgular, kayıp yok) → paket **8,9 sn**'de yeşile döndü. ✅ **Ve bu, T57 düzeltmesinin TASARLANDIĞI GİBİ çalışmasıdır:** eskiden bu durum **deadlock** verip *"append-only bozuldu"* diye okunurdu; şimdi okunabilir bir mesaj veriyor |
| **⚠️ İki bilinen flake** | İkisi de **M6-01 kaynaklı DEĞİL**, ikisi de **önceden var**: (1) `TestPolicySetDB_ConcurrentFirstTapsMaterialiseOnce` — M7-03 devrinin (`EnsureBaselinePolicy` eşzamanlılıkta 23505) test yüzü, ~26 koşuda 2; son 8 kapanış koşusunda **0**. (2) **bağlantı tükenmesi** (`FATAL: sorry, too many clients already`) — `max_connections=100` − 3 rezerve = **97**, ve `internal/db` + `internal/sun`'ın kırmızı-çizgi yarış testleri **54'er** bağlantı açıyor = **108 > 97**, yani **tek başlarına** sınırı aşıyorlar. Goroutine sayısını düşürmek bir **§4.4 testini zayıflatır** → düzeltilmedi. **Sonuç: `make check` yeşilliği bu iki testin zamanlamasına bağlı; kırmızı görürsen ÖNCE hangisi olduğuna bak.** |
| `make simulate-day` | KF St Julians'ta bir gün: `PASS`, ~64 sn (~62'si ADR 0006 beklemesi). **`make seed` yapılmış olmalı** |

⚠️ **`make check` son adımı `git diff --exit-code`.** Commit edilmemiş iş varken **exit 2** verir ve bu
**bilgi taşımaz** — fmt/lint/test geçmiş olabilir. Bir ajan *"make check kırmızı"* diyorsa hangi adımın
düştüğünü söylemeli; iş bitmiş sayılmadan önce **commit sonrası** exit 0 görülmeli.

⚠️ **`make test` kullan, çıplak `go test` DEĞİL.** Makefile `.env`'i yüklüyor
(`-include .env` + `export`); çıplak `go test ./...` `DATABASE_URL` olmadığı için **her DB testini
sessizce SKIP eder** ve §4.4/§4.5/§4.6 hakkında hiçbir şey kanıtlamadan yeşil verir (M5-05 denetimi).
Bir iddia "N test geçti, 0 SKIP" diyorsa **hangi komutla** ölçüldüğünü söylemeli. M5-08'de bir mutasyon
kolu çıplak koşuda **"ok"** verdi (2,5 sn), `make test` ile **24,6 sn** ve gerçek sonuç.

⚠️ **Dev-DB birikimi artık her `make test` koşusunda ~44 satır.** M5-08'in 20 002 satırlık kalıntısı
`make db-reset` ile temizlendi (2026-08-02), ama gün simülasyonu her koşuda **31 `transactions` + 11
`employees` + 2 `audit_log`** ekliyor ve bunlar §4.3/§4.6 gereği **silinemez** (kusur değil, garanti).
Üretilen çalışanlar **koşum damgalı** (`Maria Borg [sim 08-02T00:40:24 f4ef]`) → seed'li kadroyla
karışmıyor; ama damga `full_name` üzerinden **kullanıcıya görünen yüzeye** (aktivasyon ekranı, M6
dashboard) sızar, yani bugünkü dev-DB **ekran görüntüsüne hazır değil**. Demo öncesi `make db-reset`.
**CI etkilenmiyor** — workflow `make migrate`/`make seed` koşmuyor (aşağı: bu yüzden 140 test CI'da
FAIL ederdi; **önceden var olan** durum, M5-09'un gerilemesi değil, sahibi **M8**).

**Zorunlu env değişkenleri** (eksikse başlangıçta panic — bilinçli, §config): `DATABASE_URL` ·
`DATABASE_MIGRATE_URL` (farklı olmalı) · `TAPPA_SESSION_HMAC_KEY` · **`TAPPA_INVITE_HMAC_KEY`**
(M5-02; oturum anahtarıyla **aynı olamaz**) · `TAPPA_TAG_KEK` · **`TAPPA_RETENTION_YEARS`** (M5-02) ·
`TAPPA_ENV` ∈ {dev, staging, prod} · `TAPPA_TRUSTED_PROXIES` (varsayılan rota **prod'da hata**).

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
| M2-04 | Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma | **done** | `88c6036` · **iki denetçi ONAY** · tek-indeksli 8-byte kısaltma · `ConstantTimeCompare` (R7) · %98.9 kapsam · 🔴 **DÜZELTMESİ YANLIŞTI — M2-08 (2026-08-19) geri aldı.** 1. tur RED'i doğruydu (sayaç ters), ama sevk edilen "yapısal düzeltme" (`sv2()` ham `ctrBytes`'ı **verbatim** kullanır) **kusurun ta kendisiydi**: AN12196 s.10 sayacı SV2'ye LSB-first, URL'ye MSB-first koyuyor. 2. turun *"bağımsız Python CMAC ile kanıtlandı"* onayı **kanıt değildi** — Python, kodun kendi sırasını yeniden uyguluyordu. **Değer-endian M2-07'ye ertelendi** dendi, M2-07 de **M8-05'e** erteledi; boşluk 22 gün yaşadı ve `sv2()` yalnız iç-tutarlı bir goldenla korundu. Ayrıntı: M2-08 |
| M2-05 | Anahtar sarmalama (KEK) | **done** | `0d23d30` · **iki denetçi ONAY** · `Wrap(kek,uid,key)`/`Unwrap(kek,uid,ref)`+`Zero()` AES-256-GCM · AAD=UID taşınabilirlik-koruması (uidA→uidB unwrap hata) · 44-byte düzen · **KEK parametre (cache yok)** · AES-256 zorlanıyor (downgrade önlenir) · düz-anahtar/KEK sızmaz (mutasyonla) · %96.1 kapsam |
| M2-06 | Atomik sayaç ilerletme ve eşzamanlılık testi | **done** | `2092796` · **iki denetçi ONAY** (§4.4 en kritik) · `sun.AdvanceCounter` M1-08 atomik CTE'sini kullanır (verify'dan ayrı) · **50-goroutine `-race` → tam 1 kazanan** (her iki denetçi kendi koştu) · **negatif kontrol yeniden üretildi** (TOCTOU→50 kazanan) · strict `<`, 0-satır→ErrReplay, gömülü eşik yok, R4 temiz · %96.3 kapsam |
| M2-07 | sun.Verify ve test vektörleri | **done** | `cd639f5` · **iki denetçi ONAY** · `Verify` tüm zinciri birleştirir (resolve→retired/lost→QR→unwrap+verifyMAC+Zero→**sonra** advance) · `Result` döner **verdict vermez** · vaka tablosu tam + N-goroutine tam-1 (`-race`) · sıra kanıtı (kötü CMAC→advance yok) · §4.7 no-leak mutasyonla · %96.5 kapsam · **self-consistent vektör** (gerçek çip M8-05'te) · 🔴 **VE O ERTELEME BİR KUSURU SAKLADI — M2-08 (2026-08-19).** *"Gerçek çip M8-05'te"* dendi, ama dış kanıt için **çip gerekmiyordu**: NXP AN12196 s.10 anahtarı, UID'yi, SV2'yi ve oturum anahtarını **yayımlıyor**. 22 gün boyunca `sv2()`'yi yalnız kendi zincirinden üretilmiş bir golden korudu ve o golden **yanlış bayt sırasını çiviledi** |
| M2-08 | SDM sayaç bayt sırası + dış kaynaklı KAT | **done** | `<M2-08>` · **üçüncü göz 2 tur ONAY + `tappa-security-auditor` ONAY** · 🔴 **sevk edilmiş kripto kusuru düzeltildi:** `sv2()` sayacı SV2'ye **LSB-first** yazıyor (AN12196 rev. 1.8 §4.3 tablo 2 s.10; URL **MSB-first**, ikisi kasten ters) — verbatim hâli gerçek çipin **ilk 255 tap'inin hepsini** reddederdi (3 baytta palindrom 1/256, 1..255'te **hiç yok**) · `internal/sun/an12196_kat_test.go` **belgeden transkribe** 6 test, `referenceMAC`'e **hiç** dokunmuyor; 11 sabitin 10'u birebir, 1'i **"DERIVED" diye etiketli** · `sun_vectors.json` yeniden üretildi + `openssl` **tarifi** (ikinci uygulama) · **değer-endian ekseni de kapandı** (`beUint24` zaten doğruydu, FLAG silindi) · sabit-zamanlı karşılaştırma artık **kaynak okuyan bir testle** çivili (mutasyon → 4 assert) · runbook tarayıcısı bölümün **varlığını** doğruluyor (taban + çapa cümleleri) · %97,0 kapsam · **1725 PASS / 0 FAIL / 0 SKIP** · ⚠️ **kalan iş encode tarafında:** çipin URL'ye MSB-first yazdığı **gerçek silikonla** doğrulanmadı → M8-05 FAZ B |

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
| M4-03 | Decide(): bağlam kurma ve kararın uygulanması | **done** | `bfbbf77` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R8, §4.6/§5, kırmızı çizgi ihlali yok) · `Decide` Input→policy.Context kurar (ipMatch/gpsMatch/gpsDistanceM/gpsConflict/ctrGap/sunValid/channel/tag/employee/location) → `policy.Evaluate` **tek çağrı** (if-zinciri/erken-return YOK) → effect→verdict; **no-session→redirect+kayıt-yok** (§5.3 tek istisna); row7→flag (asla reject); boş set→flag · **R8:** deactivated+invalid-SUN→sun-invalid+Security=false (üçüncü göz erken-return mutasyonuyla, security-auditor kod-okumasıyla) · **marker-hilesi iki yönlü doğrulandı** (SessionTenantID=Employee!=nil işareti→sys:no-session; TagTenantID nil→sys:tenant-mismatch inert) · §4.7 mesafe/ham-koordinat değil · saf (tap→policy/geo, store/db/sun yok) · %95.7 · **PolicySet Input alanı + Decision explainability alanları (M4-02 kart düzeltildi)** · **🔴 N5→M5 bloklayan tenant devri** (aşağı) |
| M4-04 | Yön tayini (in/out) | **done** | `703d3d1` · üçüncü göz **1. turda** ONAY (**4 mutasyon** öldürüldü: toggle-ters/stale-not/practice-guard/Type-yay) · `Decide` `Decision.Type` saf toggle (LastOpenIn varsa out, yoksa in) · **takvim-günü filtresi YOK** (bağımsız cross-midnight/ay/yıl/artık-gün + 5h fark 400 gün sabit; Rusty Bar 18:05→02:10 out) · stale **>18h** (strict) → out+note (asla sessiz in) · **practice LastOpenIn → in muamelesi** (eğitim tap'i gerçek check-in'i açık tutamaz, M4-06 saat-şişirme) · Type yalnız ok/flag (reject/ignored/redirect→nil) · UTC saf süre, sabit-Now determinizmi · saf (time.Now yok) · %95.1 |
| M4-05 | Vardiya çözümü ve geç kalma | **done** | `63f6b4a` · üçüncü göz **1. turda** ONAY · **DST bağımsız yeniden hesaplandı** (denetçi Python `zoneinfo`: mart 09:15→15 geç, ekim 09:20→20 geç, overnight 01:00→420; naif midnight+offset bug −45/80 mutasyonla yakalandı) · `time.LoadLocation("Europe/Malta")` (sabit ofset yok), **tzdata tap paketine gömülü** (tek binary) · **geç kalma RAPOR-only** `Decision.MinutesLate *int` (nil=hesaplanmadı, int dakika **float yok** §6, Evaluate SONRASI, context'e girmiyor, hiçbir baseline time:minutesLate okumuyor → 180-geç→OK) · yalnız check-IN'de · çapraz-lokasyon Q17 (`employee:crossLocation`→base:cross-location-note + `Decision.CrossLocation`, geç damgası yok) · Shift==nil VE boş-tz→nil (LoadLocation("")→UTC tuzağı guard'lı) · %96.4 · cmd/tappa dokunulmadı |
| M4-06 | Trust puanı, QR kanalı, practice tap | **done** | `a82dfa8` · üçüncü göz **1. turda** ONAY (2 mutasyon: trustScore sabit / isPracticeTap false → RED) · Trust `20+50(IP)+30(GPS)` verdict switch ÖNCESİ, **verdict'ten bağımsız** (reject 70 > ok 50) · **Practice sunucu-türev** (`ActivatedAt`+`LastForPerson==nil`, ok/flag'te) — **client alanı YOK** (reflection guard `TestInput_HasNoClientPracticeField` yeni alanı yakalıyor), checkout asla practice → **saat-şişirme exploit'i yapısal kapalı** · QR uçtan uca (base:qr-requires-ip): QR+IP-yok+**GPS-var→flag** (Q15), QR+IP→ok, SUN-suz QR sys:sun-invalid'e takılmaz (NFC-only) · manuel SUN atlar; entered_by M5-05 yazma-yoluna ertelendi (kart+M5-05 kriteri eklendi, Decide saf func hata dönemez) · %96.7 · **sertleştirme notu: isPracticeTap'e LastOpenIn==nil (savunma-derinliği, client-erişilemez)→M4-07** |
| M4-07 | Tablo bazlı test seti | **done** | `c5536be` · **iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R6/R8) · `table_test.go` duplikasyon-ledger'ı: §5 yedi satır (`TestDecide_Section5Rows`) + 5 zorunlu ek vaka · **debounce KİŞİ-bazlı** vaka (farklı kişiler aynı plaket 10sn→hepsi ok) — person-scoping mutasyonuyla RED kanıtlandı (merkezi) · mobil-veri (ok/trust 50/not "verified via GPS" baseline'dan) · Rusty Bar gece turu cross-midnight · deaktive→reject+Security · **R8** Evaluate tek çağrı erken-return yok (redline temiz), **R6** row7-flag/no-session-tek-redirect/default→flag · **isPracticeTap sertleştirmesi** (+`LastOpenIn==nil`, revert→RED, kayıt yazımını etkilemez §4.6) · %96.7 kapsam · guardrails.go:222 yorum-notu→internal/policy sonraki dokunuş |

### M5 — [Tap akışı](m5-tap-akisi.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M5-01 | internal/session: oturum yaşam döngüsü | **done** | `a71e1b2` · **iki denetçi ONAY** (genel üçüncü göz **3. turda** + `tappa-security-auditor` kapanış turu) · **5 tur, 2 RED, ikisi de aynı sınıf: "yorum, kodun sağlamadığı garantiyi beyan ediyor"** · **RED-1** `Token` unexported alanda `%v/%+v/%#v/slog` ile ham token bastı (`fmt`, `CanInterface()==false` olunca `Formatter/Stringer/LogValuer` **atlar**) → `struct{ v *string }`; kapanış denetçisi **17 taşıyıcı × 18 render = 306 ölçüm, 0 sızıntı** + pozitif kontrol (çıplak `string` 18'in 13'ünde sızdı) · **RED-2** `Cookies` sıfır değeri (`var c`, `Cookies{}`, yazılmamış struct alanı — Go'da yasak olan alanı **adlandırmaktır**) prod'da `Set` **ve** `Clear`'da Secure'suz çerez yazdı → kutup çevrildi `struct{ insecure bool }`; 3. tur denetçisi **17 sıfır-değer yolu + 99 Env×BaseURL** kombinasyonunu yenemedi · `Verify` **tek sorgu** gerçek Postgres'te iki yolla ölçüldü (`pg_stat_user_tables` Δ + `log_statement=all`) · RLS izolasyonu **non-vacuous** (3 denetçi ayrı ayrı `DISABLE ROW LEVEL SECURITY` → RED → geri) · 5 sorguda açık `tenant_id`, DELETE yetkisi `f` · §5 satır 3/4 **korundu**: `Verify` iptalde **dolu `Resolved` + `ErrRevoked`** · sqlc çıktısı bağımsız yeniden üretilip **bayt bayt** eşleşti · kapsam **%94.0** (`deviceLabel`, `NewCookies`, `Secure` %100) · **kapsam genişlemesi:** `TAPPA_ENV` kapalı küme (`internal/config`) · **migration YOK** (00003 zaten var) · **Q11 AÇIK** (gerçek iPhone — yukarı) · 7 devir → "M5-02/M5-03'e devralınan" |
| M5-02 | Davet ve aktivasyon akışı | **done** | **A fazı** `9139ee7` · **B fazı** `0601b6d` · **iki fazda toplam 5 denetim turu, 3 RED** · **A:** `employee_invites` (RLS beşlisi, `password_resets` kalıbı) + `resolve_invite_by_code_hash` (ADR 0002 md.7 **üçüncü** resolver) + **kaynaşık CTE `ConsumeInviteAndActivate`** (iki ayrı sorgu "aktive ⇒ davet tüketildi"i çağrı sırasına bırakıyordu = hayalet-çalışan) + iç CTE'de **`EXISTS` guard'ı** (veri-değiştiren CTE koşulsuz çalışır → deaktif çalışanda davet **yanıyordu**; COMMIT ile ölçüldü, guard'la `burned=f`) + **sütun-düzeyi `GRANT UPDATE (used_at)`** (diriltme/kaydırma/hash-yeniden-yazma üçü de `permission denied`) · **A-RED:** kart CHECK'in **kod entropisini zorladığını** sanıyordu — `sha256('123456')` da 64-hex, tel-tuzak hiç ateşlenmiyordu → yükümlülük **Kabul kriterleri**ne taşındı · `FOR SHARE` **ölçümle reddedildi** (40P01 deadlock, iki cihaz aynı çalışanı aktive ederken) · **B:** `internal/{invite,audit,handler}` + ilk `.templ` sayfaları + `00010 locations.wifi_ssid` · **alan ayrımı çift** (`TAPPA_INVITE_HMAC_KEY` + etiketli girdi; aynı anahtar altında bile session yapısından farklı, ölçüldü) · **B-RED 1: aktivasyon-fixation** (SameSite çerez **yazmayı** kısıtlamaz → çapraz-site GET saldırganın kodunu ekiyor, sonraki GET **başka tenant'ın** formunu render ediyor, `Submit` mevcut oturumu görmediği için kurbanın oturumu **sessizce eziliyordu**) → CSRF token + 409 + koşullu ekim-reddi, **5 mutasyonla** kanıtlandı · **B-RED 2: sınırsız SİLİNEMEZ `audit_log` yazımı** (300 istek → 290×429 ama **300 satır**; `audit_log` append-only, `tappa_owner` bile silemiyor → tek ölü davet linkiyle izin şişirilmesi; §4.6'nın koruduğu iz kendi bağışıklığıyla silah oluyordu) → **üç bütçe** (flood/unknown/invite), 300→**11 satır** · **M5-01'in RED'i yeni pakette yeniden üretilmişti** (`activationState.code` çıplak `string`, `%+v` ham kodu basıyordu) → `invite.Code` · `heldBy` **fail-closed** (DB hatası "oturum yok" değil "bilmiyorum") · GDPR Art.13 **config'den** render (koda hukuki sayı gömülmedi, Q13→backlog B3) · **davet üreten HTTP uç noktası YOK** (admin auth M6; kimliksiz uç nokta Y-D'yi genişletirdi) · **7 aşırı iddia ölçümle çürütülüp indirildi** · **§5 satır 3 BAĞLANMADI** (hedef var, yönlendirme M5-04) · handler+invite+audit **64 test**, 0 SKIP · Node yok |
| M5-03 | Middleware: gerçek IP, tenant, oran sınırı | **done** | `1fdd1ad` · **iki denetçi**, **2 RED** (üçüncü göz + `tappa-security-auditor`) · `internal/httpx/{realip,identity,ratelimit}.go` · **XFF SAĞDAN sola**, tüm başlık örnekleri, güvenilmeyen peer → başlık **hiç okunmaz**, `TrustedProxies` boş → `RemoteAddr`; chi `RealIP` **kullanılmadı** (koşulsuz güvenir; §5'te IP = **50 puan**, sahtelenebilir adres hiç adres olmamasından **kötü**) · **RED-1:** *"tek otorite / handler kazara ham başlığa uzanamaz"* yanlıştı — `Forwarded` (RFC 7239)/`CF-Connecting-IP`/`X-Client-IP` handler'a ulaşıyordu → strip listesi **3→32**, **canlı TCP soketiyle** ölçüldü (36 adayın **23'ü** geçiyordu → **4**), iddia denylist kapsamına indirildi; kalan 4 **pozitif kontrol** (`Via`, `X-Forwarded-Host/-Proto` adres değil; **`Origin` CSRF için taşıyıcı**, silinse aktivasyon kırılır) · **RED-2 (aynı kapının içinde):** varsayılan-rota kapısı **HAM** prefix'e bakıyordu ama normalizasyon 4-in-6'yı unmap ediyor → `::ffff:0.0.0.0/96` kapıdan `/96` geçip çözücüde **`0.0.0.0/0`** oluyordu; prod'da **sessizce**, her çağıran kendi adresini seçebiliyordu → **ikinci temsil silindi** (config v4-mapped yazımı reddediyor, httpx düşürüyor) — iki kanonikleştirme hatanın **kaynağıydı**, ve `config→httpx` import döngüsü yüzünden normalizasyon config'e taşınamıyor · **kart iki yerde düzeltildi:** klasik tenant middleware'i tap yolunda **kurulamaz** (tenant çözümlemenin **ÇIKTISI**, ADR 0002 md.7; girdi alan middleware çağıranın kendi tenant'ını adlandırmasına izin verirdi) → `httpx.Identify` **yalnız gerçekleri** taşıyor, **sıfır değeri `SessionUnresolved`** (M5-01 kutup dersi; middleware'i unutan rota **gerçek oturumsuz tap gibi görünemez**), `BySession` o durumda **500** ama `SessionAbsent` **geçiyor** (§5 satır 3 meşru); 429'da `audit_log` **yalnız kimlik sonrası** mümkün (`tenant_id` NOT NULL + FK, **uydurma tenant YOK**) · **devralınan yükümlülük kapandı:** `handler.clientIP` artık `httpx` üstünden → proxy arkasındaki çağıranlar **ayrı kova** (negatif kontrol: çözücüsüz = M5-02 hâli → 429); `floodLimit` **600'de kaldı** (düzeltilmesi gereken sayı değil **anahtardı**) · **`tapSessionLimit` ilk taslakta 120'ydi, yapıcının KENDİ testi çürüttü** (5 sn'lik yenileme döngüsü tam 120) → 300 · **TapLimiter monte EDİLMEDİ** (`/t`, `/api/checkin` yok) · **N5 yalnız oturum yarısı** — `sys:tenant-mismatch` hâlâ ölü, "kapandı" **denmiyor** · 393 test 0 SKIP, httpx %95.4 |
| M5-04 | GET /t: tap sayfası | **done** | `cfa6cd5` · **iki denetçi**, üçüncü göz **RED** · **§4.4 yeni giriş noktası:** kart "sayfa açılışında ilerletme" istiyordu ama `sun.Verify` 6. adımda ilerletiyor → `PreviewWithoutReplayProtection`. **Caydırıcı yapısal:** `Preview` ≠ `Result` (atanamaz → `Verify`-şekilli kod **derlenmez**), `SUNValid` alanı **yok**, ve denetimden sonra `db.ResolvedTag` da **taşınmıyor** (`pv.CMACValid && p.Ctr > pv.Tag.LastCtr` yazılabilir bir cümleydi = §4.4'ün yasakladığı TOCTOU; ayrıca KEK-sarmalı anahtar handler'a gidiyordu). Kalan 4 alan. **İki denetçi de beş caydırıcıyı kendi paketinden derleyerek denedi → hepsi derleme hatası** · **🔬 güvenlik denetçisi bağımsız RFC 4493 + NXP SDM türetmesi yazıp GERÇEK geçerli SUN URL'i mintledi:** 30 açılış → `last_ctr` **0→0**, aynı URL `Verify`'dan geçince **700→701 `SUNValid=true`**, replay → false. **M2-04'ün "iç-tutarlı vektör byte-sırası hatasını yakalayamaz" dersine karşı DIŞ doğrulama** → yapıcının açık bıraktığı geçerli-CMAC boşluğu **ölçümle kapandı** · **RED:** `NewTap` `TapLimiter`'ı **`Audit` olmadan** kuruyordu → **M5-03'ün ONAYLANMIŞ kriteri üretimde ölüydü** (15×429, çözülmüş `employee_id`, `audit_log` 4145→4145). **Hata kimsenin dosyasında değil, iki görevin ARASINDAydı** · **🔁 ve düzeltmenin ilk mutasyonu YEŞİL kaldı** — red testleri `tp.limiter`'ı **kendileri** kuruyordu (`Audit`'i açıkça vererek): *denetlediği şeyi kendisi kuran test hiçbir şey denetlemez*. Yapıcı kendi raporladı; testler artık **ürünün kurduğu** limiter'ı üretim bütçesiyle sürüyor. Aynı tuzak **bir alan yanında** tekrar bulundu (`Refused: nil` de yeşil kalıyordu) → o da kapatıldı · **imzalı bağlam:** çipin MAC'i sunucuda **bir kez** kontrol ediliyor, sayfaya **tek bit** geçiyor; türetilmiş anahtar + oturum id'si AAD; **dokuz sahtecilik denemesi** reddedildi, sabit zamanlı, TTL 15 dk + kayma fail-closed · **`ctr`/uid bağlamda SEYAHAT EDİYOR** (ikisi de adres çubuğunda zaten var), **CMAC etmiyor** — tersini söyleyen üç yer düzeltildi · §5 satır 3 (oturumsuz/iptal → 303, `transactions` sabit) ve satır 4 (deaktif çalışan canlı oturumla **sayfayı görüyor**, kaydı POST'a kalıyor) canlıda doğrulandı · **fontlar self-host** (6 woff2, **79.032 bayt**, latin+latin-ext Maltaca için, 2 OFL + sha256 provenance; **mutlak URL 0**) · `/static` dizin listelemesi kapatıldı, tap yanıtlarına **CSP**, `watchPosition` **0** · 933 test 0 SKIP, `internal/sun` %96.7 |
| M5-05 | POST /api/checkin: orkestrasyon | **done** | `b82c9f2` · **iki denetçi ONAY** · **M3+M4 İLK KEZ gerçek HTTP isteğiyle çalıştı**; §5'in **yedi satırı da** uçtan uca kanıtlandı (güvenlik denetçisi altısını **kendi HTTP sondasıyla** yeniden üretti) · **🔴 ölçülmüş bloklayan:** seed'li tenant'ta `policies`/`policy_versions` **0/0** → baseline katmanlı kararlar **`23503`**; guardrail satırları (1–5) yazılabiliyordu çünkü `version_id NULL` taşıyor → **delik tam olarak satır 6–7'deydi: sıradan `ok` ve `flag`, ANA YOL**. M7-03 hiç çalışmamış. Çözüm: **ilk ihtiyaçta idempotent materyalizasyon** (uuid-v5 türetilmiş id → conflict hedefi **var olan kısıt**, migration yok; `policy_versions` append-only korunuyor, `max version_no = 1`). Alternatif ("gürültülü başarısız ol") ya tap'i düşürmeye (§4.6) ya da o tenant'taki **her** tap'i review'a göndermeye indirgeniyordu · **🔴 N5 KAPANDI** — `tap.Input` iki tenant'ı taşıyor, `sys:tenant-mismatch` **ateşliyor** (403, iki tenant'ta da **0 satır**, yabancı `last_ctr` sabit, gövdede yabancı id/uid/mekân adı **yok**; kararı **guardrail** veriyor, FK değil) · **F1 (denetimden):** advance karşılaştırmadan **ÖNCE** ve **yabancı tenant'ın RLS bağlamında** koşuyordu → çapraz-tenant tap yabancı `last_ctr`'ı **900→901** yapıyor, o tenant'ta **hiç iz bırakmıyordu**; karşılaştırma öne alındı · **yapıcı state.md'nin N5 ifadesini DÜZELTTİ:** beslenmemiş hâlde *yazma* çapraz-tenant `ok` üretmezdi — `transactions_tag_fk` **23503** → 500 → **kayıt kaybı**; şema **sessiz bir ikinci ağdı** ve izolasyon ihlalini §4.6 kaybına çeviriyordu · **🔁 en değerli bulgu bir KANIT boşluğu (bu sınıf oturumda 3. kez):** üretim yazma yolundaki **gerçek TOCTOU** tüm suite'i **yeşil bıraktı** — `internal/sun` `AdvanceCounter`'ı kanıtlıyor, bu paket tap'in doğru kayıtla bittiğini kanıtlıyor, ama **çağırdığını pinleyen hiçbir şey yoktu**; mutasyon ikisinin **arasından** geçti (80 ms pencere → **12/12 SUN-valid**). Çözüm: yarışı daha çok test etmek **değil**, **çağrıyı pinlemek** (tam 1 `AdvanceTagCounter`, doğru tenant/uid/ctr, tag tenant'ının `WithTenant`'ı içinde) · **F2:** `sys:tap-freshness` **ÖLÜYDÜ** (`tap:pageAgeSeconds` beslenmiyordu); beslendi — bandı varsayılanlarla **boş** (TTL 900 == eşik 900), bu yüzden hiçbir test yakalamamıştı; M5-10 pencereyi daraltınca **erişilebilir** olacak · **F3:** N3 wiring'i **yanlışlanamazdı** (harness debounce'u varsayılana eşitti → mutasyon yeşil); harness 120 sn'ye çekildi · **F4:** dört verdict damga sınıfı **derlenen CSS'e hiç girmiyordu** (Tailwind globları **Go dosyalarını taramıyor**) → literaller `.templ`'e taşındı · **K1:** sıfır-zaman nöbetçisi `0001-01-01T00:00:00Z` ile çakışıyordu → `OccurredAt` **pointer** oldu · N1/N2 (16 sahte alan yok sayıldı)/N4 + `ErrUnknownTag` + `entered_by` + `sys:occurred-at-bound` hepsi ayrı kanıtlı · **dayanıklılık:** 9 düşmanca girdi (yıl 9999/yıl 1/±23:59 offset/±180/denormal float) → **hepsi 200 + tam bir satır, hiç 500 yok**; politika katmanı zorla düşürülünce **kayıt yine yazılıyor** (`flag`/`default`) · dokümanlar `redirect`/`ignore` beyan **edemiyor** → tenant politikası tap'i **susturamaz** · 1033 test 0 SKIP, tap %97.0 / sun %96.7 |
| M5-06 | Onay ekranı ve marka mesajları | **done** | `b3fb2b5` · **iki denetçi ONAY** (`tappa-security-auditor` + genel üçüncü göz) · **15 tur, 11 RED — projenin en uzun görevi ve neredeyse HEPSİ tek sınıftan: bir cümle ya da bir SAYI, sistemin vermediği bir şeyi beyan ediyor** · **RED-1 (§4.6, güvenlik denetçisi):** `ignored` ekranı *"Your earlier tap stands."* diyordu — debounce **verdict'ten VE kanaldan bağımsız** (`GetLastTransactionForEmployee`'de yüklem yok → `decide.go:180` koşulsuz → `guardrails.go:328` yalnız gap), yani öncül `flag`/`reject` olabilir. **Görevin `flag`'den sildiği sessiz onay kusuru yok olmamış, `ignored`'a TAŞINMIŞTI** · **RED-2:** `reject` başlığı `<h1>`'de *"Not recorded"* diyordu; `Record` INSERT'ten **sonra** hiç hata döndürmüyor (`checkin.go:569-602`) → render edilen Result **satırın kanıtı**; aynı sayfa dört satır altta "was recorded" diyordu ve yanlış cümleyi **hiçbir test yasaklamıyordu** → *"Not counted"* · **RED-3…9 hep ARACIN kendisinde:** elle kurulmuş bayt-golden üretimin hiç render etmediği bir gövdeyi pinliyordu (Note'suz **971 B** vs gerçek **1061 B**) → metin-düğümü beyaz listesi **dört kanaldan** yenildi (CSS `content`, `</main>` dışı, `aria-label`, `title`) → `<input readonly value>` (*"value machine-facing'dir"* yanlıştı) → `<iframe srcdoc>`/`<object data>`/`<img src=data:svg>` → `<link href="data:text/css,…{content:'…'}">` (izinli eleman + okunmayan öznitelik) → metin testi retry dalını **hiç render etmiyordu** + `<meta http-equiv=refresh>` → **regex öznitelik SIRASINA bağlıydı** (oturumun kanonik dersi, kendi kontrolünün içinde) · **son hâl: üç dar beyaz liste** — görünür metin (doküman + 11 öznitelik) · eleman adları (**kapalı küme 16/14, iki yönlü eşitlik**) · dış referanslar (`{/static/css/app.css}`; **markanın "mutlak URL 0" kuralı ilk kez teste bağlandı**) — artı 7 tap yanıtında pinlenen CSP · **kapatılamayan 8 kanal GARANTİ değil LİMİT olarak sayıldı** (`<meta name=description>`, navigasyon yanıt başlıkları (`Refresh:` ölçüldü), CI'da **daima SKIP** olan CSS kontrolü, runtime script, beş aktivasyon ekranında CSP yok, elle düzenlenmiş `*_templ.go`, ekran-başına elle kapsam) · **wiring boşluğu ayrıca kapatıldı:** altı hata ekranının metnini yalnız elle kurulmuş bir view pinliyordu → `renderProblem` başka şablona/view'a bağlanınca **RED 20/17** (önce ikisi de yeşildi) · **11 sonuç şekli + 6 hata ekranı ×2 + 5 DB alt testi**, beş not sabitinin **5/5**'i üretimden sürülüyor (`staleOpenInNote` 19 sa eski açık kayıt seed'iyle) · **kopya kararları §4.6'dan:** `flag` "All done" **demiyor**, onayı **vaat etmiyor**, itiraz kapısı açık · practice tap'te marka mesajı **yok** · `business_type` ile tenant mesajı (**seed UUID yok, migration yok**) · **sayı hataları tek başına bir bulgu sınıfı oldu** (`SEVEN`→8 · "üç aktivasyon ekranı"→5 · "dört dal"→5 · "iki vaka"→4 · "yedisini de"→6 · "~16:1"→**5.70:1**, çünkü kapanış cümlesi docket'in **dışında**) → alan sayısı artık **reflection teliyle** çivili · **denetçi ağaca iki kez zarar verdi** (biri `git checkout` ile commit edilmemiş 12 satırı **kalıcı** sildi, biri `basename` çakışmasıyla dosya ezdi ama `git hash-object` ile birebir kurtardı) → kural `agent-brief.md`'ye yazıldı · PASS **1158** SKIP **0**, `app.css` 14256 B, `make audit` 0 |
| M5-07 | Mini tur ve practice tap | **done** | `e0a5700` · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor`) · **görevin yarısı zaten hazırdı ve bunu ÖLÇMEK işin ilk yarısıydı:** `practice` sunucu türetimli (M4-06), TRAINING damgası + marka mesajı bastırması (M5-06) çalışıyordu · **YENİ:** `GET /activate/tour?step=1..3`, **sunucu render, JS yok, istemci state'i yok**, linklerle ilerliyor, her slayttan atlanabiliyor; `Submit` **ilk** aktivasyonu tura, **ikinci cihazı** doğrudan onaya yönlendiriyor · **tur hiçbir şey yazmıyor** (7 istek boyunca `transactions`/`audit_log` donuk + pozitif kontrol; `Set-Cookie` boş; POST/PUT/DELETE → 405) · **🔁 İKİ MASKELİ MUTANT bulundu ve kapatıldı** — `gather`'ın `if !open.Practice` guard'ı silinince **tüm suite yeşil** kalıyordu (motor tarafı `decide.go` bağımsız kanıtlı olduğu için: *bir garanti A'da kanıtlanıp B'de tüketiliyorsa B'nin onu KULLANDIĞI ayrıca pinlenmeli* — bu oturumda **üçüncü** kez), ve `transaction()`'ın `Practice: t.Practice` eşlemesi **eşdeğer mutanttı** (tek tüketicisi `resolveDirection`, filtre ayaktayken alan üretimde hiç `true` olmuyordu) → `TestGatherDB_…` + `TestTransaction_CarriesThePracticeColumn` · **§4.6 vaat analizi:** practice hakkı **herhangi bir** önceki satırla harcanıyor (`GetLastTransactionForEmployee`'de **verdict ve kanal yüklemi yok**): önceki yok → evet · `reject`/`ignored`/manuel → hayır; ilk tap **asla `ignored` olamaz**; QR ilk tap practice **olur** · **en kötü vaka ikinci cihaz** (zaten tap etmiş biri) **yapısal olarak** vaadi hiç görmüyor · güvenlik denetçisi `practice`'i **dört kanaldan** (query/header/multipart/JSON) hem iddia hem **reddetme** yönünde denedi ve sütunu geri okudu: `practice=false` gönderen ilk tap yine **`true`** · **davet kodu çerezi tura kadar yaşamıyor** (istek istek izlendi, `clear(w)` yönlendirmeden önce) · **RED (1 tur):** `assertRefs` href **DEĞER** kümesini karşılaştırıyor **sayısını değil** → slayta **metinsiz**, izinli hedefli üçüncü bir `<a>` (görünmez ikinci dokunma hedefi) eklenince suite yeşildi ve testin yorumu *"a link ADDED to a slide fails too"* diyordu → `TestTour_HasExactlyTheseTouchTargets` slayt başına **sıralı (hedef → etiket)** listesini pinliyor + `on…=` reddediyor · **`ping=` kapatıldı** (`refRE`'ye eklendi; M5-06'dan devralınan boşluk, ortak `Problem` şablonunda da RED → **on bir ekran**) · tur M5-06'nın **üç beyaz listesine de eklendi** (13 etiketlik kapalı küme) · **§4.4 kararı:** emekli plakete reject `last_ctr`'ı **ilerletmiyor** ve bu **doğru** (ilerletmek sayacı çipin önüne iter → sonraki gerçek tap'ler replay = kodun kendi adlandırdığı DoS); bedeli plaket dönünce bir kez `base:ctr-gap-review` · **kapsam dışı düzeltildi:** `internal/policy/document.go` *"EffectIgnore → no record"* **yanlıştı** (`ignored` satır **yazıyor**; mutasyonla 4 test RED) · Tailwind farkı **tek yeni seçici** `.min-h-11` (14283→14312), sıfır düzyazı-doğumlu ölü kural · **1197 test, 0 SKIP** |
| M5-08 | QR kanalı | **done** | `1d836e3` · **iki denetçi ONAY** · **8 tur, 7 RED — projenin en derin zinciri** · **başladığı yer:** QR motorda zaten bağlıydı (`Parse` kanalı üretiyor, `base:qr-requires-ip` baseline'da, `preview` anahtara dokunmadan kısa devre yapıyor); eksik olan **kanıttı** — bu pakette **hiçbir** `GET /t` isteği `&ctr=&cmac=` olmadan yapılmamıştı, yani varış yolu hiç sürülmemişti · **iki maskeli mutant öldü:** `preview.go` adım 4 silinince suite **tamamen yeşil** kalıyordu (sonuç aynı, saklanan fark: **doğrulanacak hiçbir şey taşımayan URL için AES anahtarı açılıyor**) ve `channel` sütunu hiç pinlenmemişti (`"nfc"` sabitlemesi her paketi geçiyordu) · **sonra ölçüm bir zincir açtı:** §5 satır 5 debounce'u — sayacı olmayan bir kanalın **tek freni** — **dört ayrı şekilde** aşılabiliyordu ve her biri ancak öncekini kapatınca göründü: **mesafe** (gap istemcinin `occurred_at`'inden) → **seçim** (`ORDER BY occurred_at DESC`, yani öncülü de istemci seçiyor: geçmişi olan çalışan + 20 geriye tarihli POST → **20 sayılan satır**) → **işaret** (ileri tarihli tap `sys:occurred-at-bound` ile reddedilir ama **kaydedilir**, sonra o sıralamayı kazanır, negatif gap guardrail'i tümden kapatır → sonraki 20 dürüst tap **`flag` değil `ok`**) → **eşzamanlılık** (`gather` ve `write` ayrı tx → 50 eşzamanlı POST **0,48 sn'de 51 sayılan satır**) · **iki kullanıcı kararı (2026-08-01):** iki koşullu debounce **şimdi**, ve **kişi başına advisory lock** · **bugünkü kural:** `gap = min(beyan mesafesi, DB'nin hesapladığı yaş)` — `clock_timestamp() − created_at`, yalnız tap kanalları, **manuel öncül muaf** (müdürün satırı dokunuş değil; düz `min` müdürün geçmişe tarihli girişinden 30 sn sonraki **gerçek tap'i yutuyordu**) — ve `gather`+`Decide`+`write` **tek transaction**, `pg_advisory_xact_lock(tenant‖employee)` altında · **ADR 0006** (ölçümler, **beş reddedilen alternatif**, ne garanti edilmediği) · **kilidin bedeli ölçüldü ve yazıldı:** bekleyen istek **havuz bağlantısı tutuyor** → tek anahtara flood, ilgisiz kişinin gecikmesini **6–9×** artırıyor (`pg_stat_activity`: **16 bağlantının 15'i** `wait_event='advisory'`, kontrol kolunda **0**) · **§4.6 penceresi büyüdü** (`advance` → havuz → **kilit beklemesi** → `INSERT`; dışarıdan 3 sn kilitte tap **3,32 sn**; tavan `middleware.Timeout(30s)` — küme/DB/rol'de `statement_timeout`/`lock_timeout`/`idle_in_transaction` **üçü de 0**) — güvenlik denetçisi **kabul edilebilir** buldu (şekil önceden de vardı; diff havuz alımını **3'ten 2'ye düşürüyor**; aynı kişinin kendi çakışması gerekiyor; kaybedilen kayıt zaten `ignored` olacak olan; sessiz değil) · **kapsam dışı düzeltildi:** `policy/document.go` *"EffectIgnore → kayıt yok"* **yanlıştı** (mutasyonla 4 test RED) · **7 RED'in tamamı "bir cümle, sistemin vermediği bir şeyi beyan ediyor" sınıfından**; son üçü **yalnız metin** ve aynı iddia (*"birkaç milisaniye"*) **altı kez** yeniden doğdu — bir kez aynı commit içinde bir dosyada geri çekilirken kardeşinde ayakta kaldı · **1250 test, 0 SKIP**, migration yok |
| M5-09 | Uçtan uca test ve "bir günü simüle et" | **done** | `b0044c5` · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor` koşulsuz) · **3 tur, 1 RED** · **önce bilinen engel:** seed `aes_key_ref`'e **42 baytlık düz ASCII** yazıyordu → her seed'li plaket NFC yolunda **500** (`sun.Unwrap` 44 bayt ister). Zarf **SQL literali olamaz** (operatörün KEK'ine bağlı + `Wrap` her çağrıda taze nonce çeker) → seed **iki adımlı**: `seed.sql` yüksek sesli placeholder yazar, `seed.sh` `go run ./test/fixtures/seedkeys | psql` ile **aynı role** (`tappa_owner`) zarfları basar; program **hiçbir yere bağlanmaz**, `(KEK, fixture listesi)`'nin saf fonksiyonu. Sahte anahtar `SHA-256("tappa-fake-seed-tag-key-do-not-use|"‖UID)[:16]` — repoda **ne düz ne sarmalı** değer var, yalnız tarif (§4.7) · **drift guard** (`DO $$ … RAISE $$`) **ilk hâlinde mutasyonda YEŞİL kaldı** (kapsamı `SeedTags`'ten türetiyordu, listeden plaket silinince guard da bakmayı bırakıyordu) → tenant çifti sabitlendi, mutasyon **RED** · **gün sıkıştırılamaz:** ADR 0006 debounce'u **sunucu saatiyle** ölçüyor → beklemesiz koşuda **15 kaydın 10'u** `sys:person-debounce` (tam günde 31'in 19'u), ve §4.6 gereği **15/15 satır yazılıyor**. **Motor fixture'a eğilmedi**, gün gerçek zamanda bekliyor (`policy.DebounceMinSeconds=30`; 30 sn altı `config.Load`'un reddettiği **dejenere değer** olurdu) · **RED (1. tur, iki madde, ikisi de belgeleme):** `day_db_test.go`'nun *"see the limits at the end"* atfının işaret ettiği bölüm **yoktu**, ve F5 (aşağı) hiçbir kalıcı yere yazılmamıştı → `LIMITS L1–L4` bölümü + kart md. 6 · **🔴 F5 — sevk edilmiş kodda §5 ihlali bulundu (yapıcının kendi bulgusu, iki denetçi büyüttü):** bir `practice` satırı **daha eski ve açık** bir gerçek girişi maskeliyor (`GetLastOpenTransaction` tek satır döndürüyor, tüketici practice ise **atıyor ve altına bakmıyor**) → çıkış `in` oluyor, giriş **hiç kapanmıyor**, **hiçbir sinyal yok**. **Düz HTTP'den erişilebilir** (`occurred_at` sevk edilmiş alan, tavan 72 sa). Denetçi bunu **günün kendi testiyle** üretti (workaround kaldırılınca `day_db_test.go:546` RED) → **kullanıcı kararı 2026-08-02: kendi görevinde düzeltilecek → M5-11** · **`assertAfterShiftStart` YAPISAL OLARAK BOŞTU** (mutasyon: `lateness → return nil` ile gün o satırdan sorunsuz geçti) → kaldırıldı, "geç kalma" senaryosu **HTTP'de üretilmiyor** diye dürüstçe sayıldı · **`assertTellableApart` dejenere değer tuzağına düştü** (beklenen de fiili de aynı `f.runStamp`'tan; `newRunStamp → "CONSTANT"` süiti **yeşil** bıraktı — *kendi dosyasının adını koyduğu tuzak, onu önlemek için yazılmış kontrolün içinde*) → saf `TestRunStampVariesBetweenRuns` + satır-sayısı kolu, mutasyon **0,96 sn'de RED** · **senaryo tablosu dürüstçe sayıldı: 12'den 10'u HTTP üzerinden, 1'i HTTP dışı** (`manual` — uç nokta **uydurulmadı**, M6-04'ün; router'ın kullandığı **aynı `checkin.Service`**), **1'i hiç üretilmiyor** (geç kalma) · **NFC'de tek stipülasyon:** sayfa gerçekten açılıyor (gerçek `Parse`+unwrap+preview) ama CMAC biti çevriliyor — geçerli CMAC üretmek SDM'nin **ikinci uygulaması** demekti (repo bunu iki kez reddetti) → **LİMİT olarak sayıldı, kapatılmadı** · **kullanıcı kararı 2026-08-02 (süre):** `make test` **tam kalsın** (98,5 sn; CI değişmedi) + **`make test-short`** (32,9 sn, **tam 1 SKIP**, yüksek sesli mesaj) — `t.Parallel()` **elendi** (3/3 kırmızı: testler **aynı seed'li plaketin `last_ctr`'ını** paylaşıyor, paralelde tap replay sayılıyor ve biri **§4.4 kırmızı-çizgi testini olmayan bir ihlalle** kızarttı) · **`make audit` bu görevde ZAYIF KANITTI:** `redline-check.sh` `SRC`'si `test/`'i **içermiyordu** → yeni dört dosyanın **ikisi** hiç taranmıyordu; güvenlik denetçisi dokuz deseni de elle koşturdu (hepsi temiz) ve `SRC`'ye `test` eklendi (**sıfır yanlış pozitif**, ölçüldü) · **1255 test, 0 SKIP** · migration **yok** |
| M5-10 | Tap tazelik penceresi (URL biriktirmeye karşı) | **done** | `68acb81` · **iki denetçi ONAY** · **6 tur, 4 RED** · **kartın yarısı gerçek dışıydı ve bunu ÖLÇMEK işin ilk adımıydı:** kart M5-04'ten **önce** yazılmış, oysa M5-04 imzalı tap bağlamını getirdi — `IssuedAt` **sunucu saati**, payload'ın 8. alanı, **MAC'in içinde**, `tap:pageAgeSeconds` → `sys:tap-freshness` zinciri **çalışıyor**. Eksik olan tek şey iki sayının eşitliğiydi (eşik 900 == `tapContextTTL` 900 → guardrail **hiç ateşlenemiyordu**). **Kullanıcı kararı 2026-08-02: `tap_page_views` tablosu YAPILMADI** — tablo **koruma tarafında sıfır** ekliyor (pencere `GET` anından ölçülüyor ve o anı **saldırgan seçiyor**), tek gerçek katkısı M6-11'in *"POST'suz GET"* metriğiydi → o karta devredildi. **Migration YOK.** Üretim: **13 eklenen yorum-dışı satır** (`config.go` 6 + `checkin.go` 7) **+ 18 yer değiştiren** (`guardrails.go`, net yeni mantık **sıfır** — asıl güvenlik değişikliği o blok taşımasıdır) · **kullanıcı kararı 2026-08-02 (§4.6 eşiği):** `tapContextTTL` **15 dk kaldı** → `<180` normal · `180–900` **kayıtlı** reject · `>900` **kayıtsız 400**, LİMİT olarak sayıldı · **🔴 asıl bulgu genel gözün DEĞİL, `tappa-security-auditor`'ın:** bandı açmak bir **regresyon** üretti — `sys:tap-freshness` sırada **#4**, `sys:employee-deactivated` **#7** → deaktif oturum **3 dakika bekleyerek** güvenlik uyarısını düşürüyordu (§5 satır 4 ihlali; ölçüm `window=15m → ALERTS +1`, `window=3m → ALERTS +0`). **Aile taraması ikinci ve DAHA ESKİ bir örnek buldu:** `sys:occurred-at-bound` de düşürüyordu ve tetiği **daha ucuz** (bekleme değil, bir POST **form alanı**) · **çözüm tek slice hamlesi:** §5'in adlandırdığı beş satır artık §5'in kendi sırasında, adlandırmadığı iki zamanlama kuralı arkada; `sun-invalid`'in ön-alması **kasten korundu** (sahte tap uyarı imal etmemeli, R8) · **ADR 0007** (ölçüm · aile tablosu · iki reddedilen alternatif **ölçümleriyle** · **beş garanti-dışı**) · **🔁 eski yerleştirme kuralı REGRESYONU KABUL EDİYORDU** (*"sırayı bozmayan her yere konabilir"* — ölçüldü: hatalı sıra da bu kuralı sağlıyordu) → yerine **yapıyı** kontrol eden invaryant: *uyarı taşıyan bir guardrail'in önündeki her şey **adlandırılmış istisna** olmalı* · **ve sabit listeli test bir AĞ DEĞİL, DEĞİŞİKLİK DEDEKTÖRÜDÜR:** yeniden sıralamayı yakalıyor ama **eklemeye karşı çaresiz** (kırmızı testin doğal onarımı listeyi güncellemektir = tam olarak yanlış hamle); denetçi bunu sahte bir 11. guardrail'le kanıtladı — **üç paket de yeşil** kalıyordu · **çapraz çarpım ölçüldü** (864 kombinasyon, 78 kazanan değişti; **uyarı kaybeden 0**, kayıt kaybeden 52'nin **hepsi** oturumsuz = §5 satır 3'e uygun **ve üretimde erişilemez**, üç bağımsız kanıtla) · kapsam dışı iki yanlış cümle düzeltildi (sahte SUN + lost tag **uyarı imal edebiliyor**; `+nan` ParseFloat'ta geçersiz) · **1289 test, 0 SKIP** |
| M5-11 | Practice satırı açık girişi maskeliyor (§5 yön ihlali) | **done** | `1a945fd` · **iki denetçi ONAY** · **2 tur** · **sevk edilmiş kodda bir §5 ihlalinin düzeltmesi**, M5-09'da bulunmuş, kullanıcı kararıyla kendi görevi olmuştu · **kusurun adı bir CÜMLEYDİ:** `decide.go` *"primary enforcement is the caller's query, **which excludes practice**"* diyordu — **sorgu dışlamıyordu**; dışlamayı tüketici yapıyordu ve yalnız **dönen tek satır** için · **düzeltme TEK SATIR** (`AND NOT t.practice`, kaynak + üretilen kodda; yorum-dışı diff **1**), şema değişmedi, migration yok · **kartımdaki senaryo YANLIŞTI ve yapıcı ölçümle çürüttü:** *"yeniden aktivasyon ikinci practice verir"* — vermiyor (`isPracticeTap` `LastForPerson == nil` istiyor **ve** `ConsumeInviteAndActivate` `activated_at`'i `COALESCE`'luyor; ölçüldü: before == after). Gerçek erişilebilir yol **geriye tarihli `occurred_at`** (72 sa, ADR 0004 §11) — yani **M9-01 kuyruğunun ürettiği şekil** · **`NOT EXISTS` practice-nötr KALABİLDİ** çünkü *"practice ⟹ `in`"* bir **invaryant**: `TestDecide_PracticeIsAlwaysAnIn` bir **özellik** testi (72 kombinasyon + iki boş-olmama sayacı), liste değil · **bitiş şartı yerine geldi:** gün testindeki workaround (`declaring(rbGPS, night.practice)`) **silindi**, `nightTimes.practice` alanı da kaldırıldı, gece vardiyası **fixture yardımı olmadan** geçiyor (`make simulate-day` PASS ~64 sn); düzeltme geri alınınca **üç bağımsız yerde** kırmızı · `LIMITS L3`, M5-09 kart md. 6 **ve** M4-04'ün *"onaylanmış ama fiilen sağlanmayan"* kriteri kapatıldı · **ADR 0008** · **🔴 güvenlik denetçisinin şartı — ADR KENDİ DÜZELTTİĞİ HATAYI TEKRARLIYORDU:** *"düzeltme yolu mekanizma olarak ZATEN VAR … + `audit_log`"* yazıyordu; ölçüldü: **408 manuel satır / 0 `audit_log` satırı / 0 HTTP rotası**. Düzeltildi — şekil §4.3'ün emri, domain yolu **var** (`ErrEnteredByRequired`); ⚠️ *(2026-08-07 güncellemesi: `audit_log` **yazma yolu** M6-04'te geldi ve review akışı onu kullanıyor, ama **manuel kaydı hedefleyen** audit satırı ve müdür giriş yüzeyi hâlâ **yok** → **M6-08**)* · **maliyet ölçüldü ve DoS değil:** yüklem indeksi kullanmıyor (`Filter`, `practice` indekste yok) ama sıradan şekilde **bedava** (11 300 → 11 300 buffer) ve practice-en-üstte şeklinde bile sıradan şeklin **altında** kalıyor (50k satırda 123–164 ms vs 150–190 ms); `Timeout(30s)`'e ulaşmak kişi başına **~3 milyon satır** = iki aylık kesintisiz flood, ve zarar **saldırganın kendisiyle** sınırlı (kişi-bazlı advisory kilit) · **1299 test, 0 SKIP** |

### M6 — [Admin dashboard](m6-dashboard.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M6-01 | Admin kimlik doğrulama | **done** | **B fazı `4bc2e72`** · **iki denetçi ONAY** · **18 tur, 8 RED — projenin en uzun görevi** · bcrypt (Q03, `golang.org/x/crypto` eklendi, `make audit` yeşil) · admin oturumu · giriş + seçici ekranları · oran sınırı · `audit_log` · **yeni migration YOK** · **beş yükümlülüğün beşi karşılandı ya da limit yazıldı** · 🔴 **beş koruma sevk edildi ve BEŞİNİN DE silinmesi suite'i yeşil bıraktı** (`isLookupableEmail` · `sessionGate` · `sameOriginGate` · `meterOnly`'nin ücretlendirmesi · `CookiePath`/`maxCandidates`'in **totolojik** testleri) — her biri ayrı turda, ayrı denetçi, **mutasyonla** · 🔴 **53× zamanlama kehaneti** (>72 baytlık parola bcrypt'i kısa devre yaptırıyordu; kayıtlı e-posta **5,53 ms**, kayıtsız **295,42 ms**, tek istekle kesin, **sunucuya maliyeti sıfır bcrypt**) — güvenlik merceği genel gözün ONAY'ından **sonra** buldu · ve düzeltmesi bir sonraki turda yakalandı (üçüncü `-short` skip'i kehanetin **iç döngüdeki tek savunmasını** sildi) · **`GET /admin` çerezsiz çağırana bütçesiz `SECURITY DEFINER` okuması ödetiyordu** (uydurma token 1,36 ms vs bozuk 156 µs, 600 istekte 0×429) · onu kapatan flood kapısı **çıkışı reddedip oturumu canlı bırakıyordu** (orkestratörün kararının regresyonu: `tap.go`'nun **ByAddress → Identify → BySession** deseninin yalnız ilk aşaması uygulanmıştı) · **sayı hijyeni altı kez bulgu oldu** (`test-short` bandı üç kez dar yazılıp üç kez tutmadı → format **gözlenen aralığa** çevrildi; 00011'in `cost-10` sayısı **~4× iyimser**, sevk edilen digest'ler cost 12) · **1633 test, 0 SKIP** · **12 limit yazılı** · *(A fazı `66d5442`, 3 tur — aşağıda)* **A fazı `66d5442`** · **iki denetçi ONAY** (genel üçüncü göz 2. turda + `tappa-security-auditor` koşullu, koşullar kapatıldı) · **3 tur** · M5-02'nin A/B kalıbı: **veri katmanı** önce, auth+ekran sonra · **🔴 kart bir şeyi söylemiyordu ve şema onu VARSAYIYORDU:** 00006 *"resolver YOK: giriş tenant'ı biliyor"* diyor ama **hiçbir şey tenant'ı kurmuyordu** (e-posta yalnız `(tenant_id, email)` içinde tekil, slug yok, tek `/api/auth/login`) → **kullanıcı kararı 2026-08-02: global çözümleme + tenant seçici** (kullanıcının kendi demo tenant'ları KF+KM **aynı kişiye ait**) · **migration 00011:** iki SECURITY DEFINER fonksiyon (ADR 0002 md.7 kalıbı; resolver sayısı **3→5**, `tappa_resolver` sütun-SELECT'i **5 tabloda 26 sütun**, tablo-düzeyi yetkisi **sıfır**) · **`resolve_admin_by_email` beş kısıttan birini BİLEREK kırıyor:** dönüş **≤1 değil N satır**, sınır kısmi unique indeksten geliyor ve **saldırgan tarafından büyütülebilir** (M7-02 kayıt açılınca) — yazıldı · **şema sertleştirmesi repoda HİÇ ADI GEÇMEMİŞ iki yeteneği kapattı:** `admin_sessions.admin_user_id`'yi yeniden yönlendirme (**yetki yükseltme**) ve `token_hash`'i ezme (**oturum ele geçirme**); sütun-kapsamlı UPDATE ikisini kapatıyor ama **un-revoke'u kapatamıyor** (*"grant hangi SÜTUN der, hangi DEĞER demez"*) → **monotonluk trigger'ı**, `tappa_owner`'ı da bağlıyor · **🔬 en ince bulgu:** `citext`'in `=` operatörü `public`'te; `search_path=pg_catalog,pg_temp` altında **görünmez** ve Postgres **hata vermeden** `text=text`'e düşüyor → kimlik doğrulama araması sessizce **harfe duyarlı** oluyor (ölçüldü: küçük/büyük harf **0 satır**). Düzeltme `OPERATOR(public.=)` + kalıcı negatif kontrol. Tuzak **`search_path` özelliğidir**, SECURITY DEFINER'a özgü değil; şemadaki diğer citext sütunu `employees.email` bugün hiçbir sorguda filtrelenmiyor → sınıf kapalı ama **kapalılık yazıldı** · **§4.7: hash artık çıplak `string` DEĞİL** — altı basma yolu (`%+v`, dilim `%v`, `%#v`, `fmt.Errorf`, unexported alan, `slog`) hash'i **verbatim** sızdırıyordu; repo'nun kendi kalıbı (`session.Token`/`invite.Code`) **üçüncü kez** uygulandı, ve **pozitif kontrol testin körü olmadığını kanıtlıyor** · `redline-check.sh` R7 desenine **`password` eklendi** (ölçüldü: 0 yanlış pozitif) — **ve yakalamadığı dürüstçe yazıldı** (altı yolun hiçbirinde `password` **kelimesi** log çağrısında geçmiyor) · **B fazına BEŞ yükümlülük**, dört yerde: numaralandırma · kukla bcrypt · oran sınırı · **bcrypt amplifikasyonu** (bir e-posta 500 tenant'ta → 500 satır, DB **0,9 ms**, ama B fazı 500 bcrypt = **~30–50 sn CPU**, **~500×**, tek kimliksiz istekten) · **🔴 aday↔parola bağı** (`tappa-security-auditor`'ın bulduğu **en ağır** madde: oturum **yalnızca hash'i eşleşen adaya** verilmeli, seçici **yalnızca eşleşenleri** göstermeli — yoksa saldırgan kurbanın e-postasını kendi tenant'ına yazıp **kendi satırında** doğrulanır ve **kurbanın işletmesini seçer**; §4.5 çapraz-tenant kimlik atlatması, canlı ölçüldü) ⚠️ ve **4. ile 5. madde birbirinin tersine çekiyor** — *"ilk eşleşmede dur"* DoS'u azaltır ama seçici tüm adayları gösterirse **tam olarak bu atlatmadır**; gerilim dört yerde de yazılı · güvenlik denetçisi **on beş** saldırı denedi (`ON CONFLICT DO UPDATE`, `MERGE`, `session_replication_role`, `pg_temp` operatör/tablo enjeksiyonu, çapraz-tenant forge…), **on beşi de bloklandı** · down/up **tam tersinir** · **1331 test, 0 SKIP** |
| M6-02 | Dashboard iskeleti ve docket bileşenleri | **done** | **`6757537`** · üçüncü göz **ONAY** · **10 tur, 5 RED** · `layout.Panel` + `TabBar` + `EmptyState` + üç CSS ailesi; **beş sekme rotası tek tablodan** `Protect()` içinde mount ediliyor (nav da aynı tablodan → *"linki 404 veren sekme"* **üretilemez**) · **kartı ölçmek iki kez kendini ödedi:** docket motifi + beş damga **zaten sevk edilmişti** (**M0 iskeleti** `7e12f37`, M5-06 değil — M5-06 yalnız **anatomiyi** değiştirmiş; perforasyon görseli **hiç var olmamış**) → üç kriter **karşılanmıştı**, iş eksik **dört bileşendi**; ve **M6-01'in bütçe borcu ölçümle kapandı** (bir sekme görüntülemesi = **1 ücretli istek**, `/static` kapı dışında → pay **11,5×**, üç sabit **değişmedi**) · 🔴 **sevk edilmiş kontrast hatası bulundu ve düzeltildi:** `.docket-label` **3,13:1** (AA 4,5:1) **12 çağrı yerinde** — **tap ve onay ekranları dahil** — artı beş ton daha **2,40–4,36:1** → **kullanıcı kararı: hepsi `ink/70`** (en kötü zemin **5,58:1**); `/60` ölçülüp **reddedildi**; wordmark **WCAG 1.4.3 logotype istisnasını REDDETTİ**, gerekçe yazılı · **ürünün İLK kontrast testi** geldi ve üç `TestCompiledCSS_*`'in aksine **CI'da koşuyor** (paleti config'den türetir, WCAG'i yeniden hesaplar, **sıfır çağrı yerinde koşmayı reddeder**, **bağlayıcı zemini pinler** — işaret silinirse de kırmızı) · ⚠️ **bağlayıcı zemin porcelain değil `green-lite`** (L 0,8229 < 0,8627); iki tur **ve** orkestratörün brief'i yanlış varsaymıştı, **türetilmiş test yakaladı** · **filtre çubuğu ve HTMX M6-03'e TAŞINDI** (kullanıcı kararı, iki kartta da yazılı) · **1647 test, 0 SKIP** |
| M6-03 | Transactions sekmesi | **done** | **`37032d0`** · **iki denetçi ONAY** (genel üçüncü göz ×2 + `tappa-security-auditor`) · **8 tur, 4 RED, 56 mutasyon** · docket kartları + altı filtre + keyset sayfalama + HTMX · **okuma yolu sıfırdan** (beş var olan sorgu tap **yazma** yolundandı) · **yeni migration yok** · M6-02'nin **üç borcu** kapandı (filtre çubuğu · HTMX+CSP **iki direktif, ikisi de tarayıcıda taşıyıcı ölçüldü** · bütçe **yeniden sayıldı, çarpan geri gelmedi**) · 🔴 **filtre çubuğu sayfanın %96'sıydı** (867 KB'ın 835'i, sınırsız büyüyen) → **kullanıcı kararı: metin kutusu + sunucu eşleşmesi** → **32 KB** · §4.5 **yedi vektörlü** çapraz-tenant saldırısında temiz (B'nin gerçek cursor'ı dahil) · §4.7 **üç duvar**, koordinat taşıyan gerçek satırlara karşı · **SQL enjeksiyonu/XSS/joker kaçağı yok** (15+12 vaka, depolanmış yol dahil) · ⚠️ **kuşak ağı tek dosya okuyordu** (pozitif kontrolle bulundu) · ⚠️ **`MATERIALIZED`'ın 27× gerekçesi GERİ ÇEKİLDİ** — DB hiç `ANALYZE` edilmemişti, istatistikler ~20× küçüktü; çit başka gerekçeyle tutuldu · **1698 test, 0 SKIP** |
| M6-04 | FLAGGED onay kuyruğu | **done** | `2e7ec64` · **iki denetçi ONAY** (`tappa-security-auditor` **VERDICT: ONAY**, genel üçüncü göz kapanışta) · **9 tur, 6 RED** — M6-01 B'den sonra **en uzun ikinci görev** · **yeni migration YOK** (00005 tabloyu + `UNIQUE (transaction_id)` + same-tenant bileşik FK + RLS beşlisini zaten taşıyordu) · karar `transaction_reviews` + `audit_log`'a **tek transaction'da** (`RecordTx`; iki yön de patlatıldı), `transactions`'a **hiç dokunmuyor** — `has_table_privilege('tappa_app')` üç tabloda da `UPDATE f · DELETE f`, `tappa_owner` denemesi bile trigger'la reddediliyor · **🔴 brief'imin ÜÇ sayısı ölçümle çürütüldü:** *"`audit_log` yolu yok"* (paket **vardı**, ~20.000 satır; doğru dar ifade `channel='manual'`-hedefli **0**), *"`transaction_reviews` 0 satır"* (**9.813** — çıktıyı `head` kesmişti, görmeden yazmıştım), *"31.193 = kuyruk"* (DB geneli; en büyük tenant **4.742**) · **🔴 güvenlik: `sameOriginGate` çözücüden SONRAYDI** (review **1** resolver okuması, logout **0**) → same-site bir sayfa 300 POST ile müdürü **10 dk kendi panelinden kilitliyordu**; `ProtectWriting` = `floodGate → sameOriginGate → requireAdmin → sessionGate`, `Protect`'in **üst kümesi**, **resolver sayacıyla** pinli — ⚠️ **M6-01 B'deki orkestratör hatasının aynısı**, kalıbın tek aşaması kopyalanmıştı · **ekran ölçmediğini söylüyordu:** ikinci karar *"başkası karar verdi"* diyordu, **kimin** verdiğini okumadan → çift tıklayan müdür kendi kaydı için o cümleyi görüyordu · **🔴 DÖRT AĞ KENDİ MERKEZÎ CÜMLESİNİ TUTMADI:** karar formu kendi URL'ini yazıyordu (`/admin/nowhere` mutasyonu **tüm paketi yeşil** bıraktı) · `layout.Panel`'in **sıfır çağıranı** vardı ama üç yorum render edildiğini söylüyordu · kuşak ağı regex'i **üç şekilde** yenildi (satır sonunda nokta · parantez öncesi boşluk · metot değeri; yüklem **tamamen silik**, `gofmt` kararlı, iki belt **yeşil**) · AST'ye taşınınca **okuyucusu** kaçırıldı (alfabetik önceki dosyada **tek yorum satırı** → sabitlenmemiş `-- name:` başka gövdeye çözülüyor, `make sqlc` kapsamsız sorguyu **üretime** yazıyor, suite **16/16 yeşil**) · **iki kullanıcı kararı:** notun render edilmesi (write-only'di, 500 karakter kırpması **görünmezdi**; sayfa 31.013 → 33.663 B, en kötü 45.563 B — yapıcının 12,5 KB tahmini **iyimserdi**) ve script'siz kabuğun **yapısal** olması (`pages.PanelShell` script parametresi taşımıyor → string düzenlemesi **derleme hatası**; `PanelShellWithScript` adını vermek hâlâ derleniyor, **yazılı**) · **kapsam genişlemesi (kullanıcı kararı):** `make check` artık `gen` koşuyor (**+10–15 sn**; yapıcının **3,34 sn**'si geri çekildi — *tek ölçümü nokta yazmak*, sayı-etiketi sınıfının **11. örneği**) · **ADR 0009** (verilmiş review geri alınamaz; §4.3'ün telafi yolu bu tabloda **yapısal olarak yok** — ihlal değil, bilinçli, ama yazılı değildi) · **varlık kehaneti kapalı ve GÖVDE BOYUYLA ölçüldü** (var olmayan uuid · başka tenant · `flag` olmayan → üçü de 303, aynı `Location`, **5.723 B**) · not render'ı 10 hasmane yükte temiz, sayfada `<script` **1** (htmx), CSP'de `'unsafe-inline'` yok · kırpma **rune sınırında** (500 kırpılmaz, 501 kırpılır) · eşzamanlılık 10 goroutine → **1 kazanan / 9 `taken` / 0 diğer / 0 yanlış atıf** · `audit_log.detail` tam **4 anahtar**, sızıntı 0 · **16 limit sayıldı, kapatıldı denmedi** · **807 üst-seviye / 1776 toplam PASS, 0 SKIP** |
| M6-05 | Employees sekmesi | **done (A+B)** | **A: `1998e89`** · iki denetçi ONAY (`tappa-security-auditor` **§4.6 RED**'i dahil) · **6 tur, 4 RED** · **migration YOK** · görev **denetim merceğine göre** bölündü (okuma yolu §4.5/§4.6/bayt · yazma yolu §4.7/yetkilendirme/oturum) · **okuma yolu sıfırdan** (`employees.sql`'de yalnız iki tap-yolu sorgusu vardı) · **🔴 güvenlik: 512 rune'dan uzun ad sayfa sınırında imleci sessizce düşürüyordu** → sonsuz döngü, **10 kişi ulaşılamaz / 50 kişi iki kez** (60 kişilik kadroda 605 rune ile ölçüldü); `full_name` şemada **sınırsız `text`**, ve **iki yazılı iddia ölçümle yanlışlandı** (*"düşürmek yalnız DAHA FAZLASINI gösterir"* — tersi; *"filtre çubuğu yankılar"* — imleç hiç yankılanmıyor). **Sınır büyütülmedi, SİLİNDİ:** imleç `?after_id=<uuid>`, adı sunucu çözüyor (**+0,44–0,99 ms**, yalnız sayfalı istekte) → **ikinci bulgu da kapandı**, hiçbir çalışan adı **URL'de yolculuk etmiyor** · **§4.7 tip duvarı çalışıyor ama YAZILI KAPSAMI yanlıştı**: `d.name` takma adı altından geçen `left(token_hash,8)` **yeni alan istemiyor** ve suite **16/16 yeşil** kalıyor → *"COVERED ELSEWHERE"* silindi, **sayıldı** (üründe sızıntı yok) · kuşak ağının kapsam iddiası **kendi komutuyla** çeliştiği için **silindi** (artık koşuda basılıyor; ⚠️ **payda kayar** — A kapanışında 43, 2026-08-08'de **47**, çünkü her görev sorgu ekliyor), `unscopedSubqueries` **21 saldırının 7'sinde** yenildi → **kapanış kuralı**: adıyla **sayıldı**, RLS'in tuttuğu **canlı ölçüldü** (`SET row_security = off` → **ERROR**) · **kadro boyu ALTI kez kaydı** ve iki tur **aynı RED'i** aldı çünkü düzeltme **yanlış katmandaydı**; sayı artık **hiçbir gerekçe cümlesinde yok**, argüman **eşitsizlikten**, ve bir **tel** şekli arıyor (⚠️ sözlük genişletmesi **30 meşru ölçümü** işaretleyip **geri alındı**; **altı kaçış** yazılı) · **kullanıcı kararları:** sayfa **50** (25'te bütçeyi aşan tek tenant kadrosunu **yürüyemiyordu**: 349 istek, 301'de 429; 50'de **0 tenant**) ve **kart koda yenildi** (deaktivasyon oturum iptal **etmez**) · **A kapanışında 1821 PASS / üst düzey 836 / 0 SKIP** · **A'da migration YOK** ‖ **B: `77dcb92` + MIGRATION 00012** · iki denetçi ONAY · **6 tur, 4 RED (İKİSİ güvenlik merceğinden)** · aksiyonlar (davet/yeniden davet · deaktive · taşıma), üç POST rotası `ProtectWriting`'de, her aksiyon audit'iyle **aynı transaction'da** · **yeni mekanizma YAZILMADI** — `ManagerVisibleChannel`/`LinkSink` dikişi hazırdı ve bu değişiklik onun **ilk ÜRETİM çağıranı** oldu · **🔴 PROJENİN İLK CİDDİ GÜVENLİK KUSURU:** bir daveti harcamak **kardeş daveti emekliye ayırmıyordu** → iki basış = **2 canlı kod**, en yenisiyle aktivasyondan sonra **en eskisi HÂLÂ aktive ediyor**, ve ikinci-cihaz yolu `RevokeAllForEmployee` çağırdığı için **gerçek çalışanın telefonu düşüyor**, eski linki tutan **onun yerine trust 100 ile mesai yazıyordu**; kötü niyetli müdür **gerekmiyor**. ⚠️ **Mekanizma M5-02'den beri şemadaydı** — onu bulduran şey kod değil **erişilebilirlik**: *"iki kez bas"* tek tıklık bir müdür işlemine dönüştü. **Migration 00012** (`cancelled_at`, sütun düzeyi GRANT, `Down` **ayrı DB'de koşuldu**, `resolve_invite_by_code_hash` DROP+CREATE'inde `OWNER`/`REVOKE`/`GRANT` **yeniden kuruldu** — DROP üçünü de sessizce atar, ADR 0002 §7) · **🔴 ŞEKİL MEKANİZMA DEĞİL, ÜÇÜNCÜ KEZ:** deaktivasyon onayı **dekoratifti** (POST doğrudan geçiyordu) ve ürünün **tek geri alınamaz** aksiyonuydu; ilk düzeltmede çerez+alan+sabit-zaman **vardı, anahtar yoktu** → denetçi **kendi çerezini basıp** geçti. `adminChoices`'ın parçaları **sayıldı** ve sayı **on değil ON BİR** çıktı (gelecek-saat sınırı, denetçi buldu) · **🔴 `ErrCodeCancelled` AĞSIZDI**: dalı silmek `default:`e düşürüyor, o da **`failAttempt` olmadan 500** veriyordu → devralma girişiminin **tek izi** tek satırla siliniyor, paket **yeşil** (§4.6 kayıt kaybı) → `switch` **tablo** oldu ve sentinel kümesi **`go/ast` ile türetiliyor**, `default:` de **yazıyor** · **iki ağın ARASINDAKİ delik** (tüketen ifadeden yüklem silinince uçtan uca **yeşil**, çünkü `Lookup` daha erken reddediyor) · **bir ağ ÜRÜNÜ değil HARNESS'ı ölçüyordu** (tek-atımlıklık testi replay'i **çerez silmeyi uygulayan** bir yardımcıdan yapıyordu) → test artık **sunucuyu** ölçüyor ve dürüst sonucu (**3 harcama**) raporluyor · **kullanıcı kararları 2026-08-08:** migration 00012 (kısa TTL **pencereyi daraltır, kapatmaz**) ve onayın **sunucuda zorlanması** · **SAYILDI:** tek-atımlıklık **istemciye bağlı** (üründe zararsız, 2..N harcama `status <> 'deactivated'`'a çarpıyor) · **1893 PASS / üst düzey 882 / 0 SKIP** |
| M6-06 | Locations & Wall Tags sekmesi | **done** | **ÜÇ PARÇA · 31 TUR, 12 RED, ALTI DENETÇİ — projenin en uzun görevi; hiçbir kusur sevk edilmedi.** ‖ **B PANEL: `4ec5e85`** · iki denetçi ONAY (**dört genel üçüncü göz, dördü de RED** + `tappa-security-auditor` **ONAY**) · **8 tur** · plaket listesi (üç liste) · **replace + mount + un-mount** · `audit_log`'dan *"kim ne yaptı"* · **yeni migration YOK**, iki yeni sorgu · **§4.7: hiçbir panel sorgusu `aes_key_ref` seçmiyor** ve **iki tip duvarı** (domain kökleri + komut tipleri; render modelleri) alanı **ad ve ŞEKİL** olarak her derinlikte reddediyor — `uuid.UUID` **kimlikle** izinli, ki **zarfın tam boyundaki `[44]byte`'ı** reddetmenin tek yolu bu; kapsamadıkları **yazılı** · 🔴 **kart düzeltildi:** *"replace tag"* tek başına envanter modelinde **uygulanamaz** (ilk plaket **mount** ister) · 🔴 **mount GERİ ALINAMAZDI ve tersini söyleyen iki cümle *"measured"* etiketi taşıyordu** — yanlış duvara mount → panelden **hiçbir kurtarma**, yedek yoksa **hiçbir kontrol**, tek çıkış **çalışan bir plaketi kalıcı emekliye ayıran** replace; **orkestratör kararı: kapı değil GERİ ALMA YOLU** (kapılamak yanlış mount'u engellemez, zarar geri alınamazlıkta) → `UnmountTagFromWall` sevk edildi, **mount kapısız kaldı ve cümle DOĞRU oldu** · 🔴 liste **çalışmayan bir `Open` bağlantısı** basıyordu (ham uid vs `ToUpper`'lı arama, harfe duyarlı `char(14)`) → sınır **ikiye bölündü** (`PlaqueUID` **yazar**, `PlaqueRef` **arar**) ve `CanonicalUID` tipiyle **derleme zamanına** taşındı · 🔴 panel müdüre **ham `plaque.unmounted`** basıyordu ve yeni rota **iki yazma-zinciri ağının da dışındaydı** → **iki türetim** (eylem sözlüğü `go/ast` ile domain'den; rotalar **gerçek router'dan** `chi.Walk` ile → **yedi rota, Faz A'nın dördü dahil**, ki hiç kapsanmamıştı) · **ikinci durma kuralı uygulandı:** üçüncü ağdan sonra kapatmak bırakıldı, kalan şekiller **ölçülmüş bir tabloda** sayıldı (altı yakalanan, **üç yakalanmayan**, ikisinin zararsızlığı **iki yönde** ölçülü) · **mekanik kural + süpürme:** *kodun sahip olduğu bir kümeyi tarif eden çıplak tamsayı yorumlarda yer almaz* → **169 aday, 17 kod-sahipli yer, 11'i sayı SİLİNEREK** · ⚠️ **yapıcının iki rapor iddiası ölçümle yanlışlandı** (*"iki paragrafı yeniden yazdım"* — yazılmamıştı; *"sayı artık hiç yazılmıyor"* — yazılıyordu ve **9 kat** yanlıştı), ikisini de sahiplendi · güvenlik merceği: **un-mount replay penceresi AÇMIYOR** (77→77→77), **10 eşzamanlı un-mount → 1 kazanan**, çapraz-tenant **aktör adı sızıntısı kurularak denendi ve sızmadı**, jeton v3'ün beş bağı probe ile kırılmaya çalışıldı · **2335 PASS / 0 SKIP** ‖ **B VERİ KATMANI: `ba671b0` + MIGRATION 00013** · iki denetçi ONAY (**üç genel üçüncü göz, üçü de RED** + `tappa-security-auditor` **ONAY**) · **7 tur** · `tags` sertleştirmesi + **envanter modeli**; **uygulama katmanı AYRI TUR, on yükümlülük miras** · 🔴 **devraldığımız ÜÇ cümle çürütüldü:** T8'in *"veri taşıması yok"*u (**18.010 küçük harfli**, **12.437'si 24.874 transaction'dan referanslı** → normalleştirme **§4.3 ihlali**, yapılmadı; **`NOT VALID`** sevk edildi, artık satırlar **donuyor** ve tap yolu **ulaşamıyor**, kirlenmenin **kaynağı kapatıldı**) · T9'un trigger koşulu (`> OLD` **retire'ı reddediyor**) · T9'un **sütun listesi** (`location_id` yoktu — *aynı günün iki kullanıcı kararı birbirini uygulanamaz kılıyordu*) · **bir GUARDRAIL fail-open'dı ve kapatıldı** (`sys:tag-not-active` **denylist**'ti, `unassigned` altından geçiyordu; **QR yolu açıktı** → stok plaketten **`flag` kaydı**; migration kendi riskini **fazla** yazmıştı — `ok` **erişilemez**) ve eşdeğerlik **migration'ın CHECK kümesinden türetilerek** ölçüldü · **⚠️ bir düzeltme aynı dosyadaki başka bir cümleyi geçersiz kıldı** (yeni kısıt, `Down`'ın kısıt **sayısını** ve **reçetesini** birden bozdu — reçete şemanın **yasakladığı** yolu tarif ediyordu) · **kapsam genişletmesi (onaylı):** `vat_number` çakışması `make test`'i **~89 koşumda bir** düşürüyordu (**%1,13/koşum, artıyor**) → 19 dosya, **9,2e-30** · güvenlik merceği: **altı bypass kapalı**, trigger `SECURITY INVOKER` + pinli `search_path`, **ctr sarması DoS değil**, taze DB'de **12/12 zarf 44 bayt** ve `VALIDATE` **başarılı** · **sayılmış limitler:** tablo geneli `tags` INSERT · **`Down` bir güvenlik geriye-gidişi** · **R4 satır-yerel** (genişletilmedi: **0 gerçek ihlal**, yanlış alarmda `AdvanceTagCounter`'ın **kendisi**) · **`FORCE` davranışsal olarak test edilemez** (sahip superuser) → katalog iddiası · **2215 PASS / 0 SKIP**, `Down` taze klonda **fresh v12 ile IDENTICAL** ‖ **A: `d010c1f`** · **iki denetçi ONAY** (`tappa-security-auditor` **VERDICT: ONAY** + genel üçüncü göz kapanışta) · **16 tur, 5 RED — projenin en uzun görevi, BEŞ ayrı üçüncü göz** · **yeni migration YOK** (son 00012; `tappa_app` iki tabloda da DELETE'e **zaten sahipti** ve RLS `FOR ALL` — **varsayılmadı, ölçüldü**) · görev **denetim merceğine göre** bölündü (A = lokasyon/departman §4.5/§4.6/yetkilendirme · B = plaketler §4.7/§4.4 **+ migration 00013'te T8+T9**) · **ALTI KUSUR:** `parseGPS` **`NaN`'ı geçiriyordu** (`NaN < -90` ve `NaN > 90` **ikisi de false** → 23514 → müdüre **500 "panel kullanılamıyor"** ve **sekiz alanın hepsi çöp**; bitişiğinde `1e-400` **sessizce 0.0'a** alt-taşıyor, yani `0,0` **ikinci bir kapıdan** geliyordu) · `venueForms` **kesilmiş dilimi** tarıyordu → tavan ötesi satır **hiçbir rotadan düzenlenemiyor** ve üstüne **yanlış** bir cümle (**M6-05 A'nın §4.6 sınıfı, tekrar**) · reddedilen **departman düzenlemesi** *"Venue could not be read"* basıyordu (`location_id` düzenlemede **hiç POST edilmiyor**; ikinci yarısı daha ağır — create'te açılır liste **kesilmiş listeden** kurulunca **başka venue seçiliyor** ve departman **sessizce yer değiştiriyordu**) · `?done=venue-deleted` **hiç olmamış bir silmeyi ilan ediyordu** ve yorumu *"başlıklar DURUM adlandırır… **bu dört**"* diyordu (dal **altı**) — **M6-05'in RED alıp kapattığı sınıfın birebir tekrarı** · çağrı-grafı ağı **paket** kapsamı iddia edip **tek dosya** tarıyordu (denetçinin `locationactions.go`'ya koyduğu **gerçek sızıntı** paketi **160,5 sn yeşil** bıraktı) · ve silme **ürünün ilk geri alınamaz yazımıydı, rol ayrımı YOKTU** (şemada `role IN ('owner','manager')` **dolu**: 30.359 owner / **7.417 manager**, **2.814 tenant'ta ikisi de**) · **⚠️ İMZA SINIFI ON BİR KEZ: bir ağın/cümlenin yakaladığı ile yakaladığını söylediği ayrı** — ve **hiçbiri üründe sızıntı değildi**, yanlış olan **yazılı kapsamdı** (NaN düzeltmesi **yanlış satıra** atfedilmişti · `no comma` testi **dizenin yokluğunu** pinliyordu · `08,00` alt-vakası **tam vakumdu** ve **davranışsal bir test onu asla göremezdi** → çağrı-grafı ağına çevrildi · §4.7 taraması **yedi yasaklı kelimeye** bakıyordu, **oturum id'si çarpmıyordu** → izinli anahtar kümesine · `created_at` ağı **anahtarı** tutuyordu **değeri** değil) · **⚠️ BİR ÖLÇÜM İKİ KEZ DOĞRU AMA POPÜLASYONU UYDURMAYDI:** *"dört `EXISTS` = 8,154 ms"* üretilemedi (**171–218 ms**) — `EXISTS`'lerin `OR`'u **kısa devre yapıyor** (9 lokasyonun 9'unda çalışan var → `transactions` **hiç değerlendirilmiyor**; sıra değişince **~125 ms**), ve düzeltmesi **tenant filtresiz tam tablo taramasıydı** → yeni kural: *bir zamanlama, tenant'ı, satır sayısı ve işlem sayısıyla yazılır ya da hiç yazılmaz* · **beş kullanıcı + üç orkestratör kararı** (A/B bölünmesi · envanter modeli · T8+T9 00013'te · **referanssıza Delete** %15,9=18.721 satır · **ondalık virgül normalleştirmesi** — yapıştırma belirsizliği **ölçüldü ve asılsız** · **C′** · **`owner`-only silme** · ret satırı `audit_log`'a ‖ ADR **gerekmiyor** (koordinat yazan **1/27**, eşik 2/27) · 00005'e **dokunulmadı**, çelişkisi `venue.go`'da atıfla uzlaştırıldı · beş limit **backlog T11–T14**) · **C′ = M6-05'in kuralı (2) harfiyen:** başlık, aktörün `audit_log` satırına karşı **aynı istekte** doğrulanıyor (tenant+actor+action+target+`at > now()-30s`, **tamamı sunucu saati**), **0,101 ms**, Index Scan, **migration yok**; yabancının URL'si **hiçbir şey** basmıyor ve beş sonda **durum+başlıklar+`Content-Length`+`Set-Cookie`+gövde** olarak **bayt bayt aynı** · **Okuma B ölçümle elendi** (jeton tek kullanımlık değil → kusuru **taşır**, backlog T12) · güvenlik merceği: **altı çapraz-tenant sondası 0 satır**, 11 yeni sorguda açık `tenant_id`, beş resolver'ın hiçbiri bu tabloları okumuyor, **tap sorguları bayt bayt**, `audit_log` append-only **`tappa_owner` için de**, jetonun **on iki parçası** teker teker · **yapıcı kendi hatasını DOKUZ kez bildirdi** (sonuncusu: `Makefile.probe` `;` ile bittiği için make **daima 0** dönüyordu → **iki mutasyon sahte YEŞİL**; kendi çıktısındaki `--- FAIL` ile çelişince fark etti, aracı yeniden yazdı, dördünü de yeniden koştu — **hepsi KIRMIZI**) · **2165 PASS / 0 SKIP · 98 test fonksiyonu · kuşak ağı 18/60** |
| M6-07 | Reports ve CSV export | **done (A+B)** | **B: `3930a52`** · **ÜÇ denetçi ONAY** · **4 tur, 0 RED** · **yeni migration YOK, YENİ SORGU YOK** (`db/queries/` ve `internal/store/` hiç dokunulmadı → `EXPLAIN` gerekmedi) · `GET /admin/reports.csv` ekranın **AYNI `ledger.Report`**'unu render ediyor, **ikinci toplama yok** ve ikisi ayrışırsa bir test düşüyor · ekranın satır tavanları CSV'de **uygulanmıyor**, ama bordro figürünün **dışladığı her nicelik** rakamlardan **ÖNCE** basılıyor · **🔴 KAÇIŞ SINIRI UNICODE'UN KENDİSİNDEN, ELLE ÇİZİLMİŞ BİR LİSTEDEN DEĞİL:** hücre başındaki **mark · format · control · `Other_Default_Ignorable_Code_Point`** dizisi `= + - @` testinden **önce** atlanıyor — ve yol buraya **üç adımda** geldi, **her adımı başka biri açtı**: yapıcı kendi ağını kurarken **üründe gerçek bir açık** buldu (`unicode.IsSpace('\x01')` **false** → `"\x01=1+1"` **kaçışsız**) · güvenlik merceği `IsControl`'ün **yalnız Cc** olduğunu ve **13 Cf runeunun çıplak geçtiğini** ölçtü **ve öldürücü kanıtı buldu: `internal/session/manager.go:512` bu sınıfı ZATEN çözmüş ve o dosyanın yorumu formül nötralleştirmeyi ADIYLA M6-07'ye devretmiş — devralan taraf sınıfın YARISINI almıştı** (*kalıbın yarısını kopyalama* sınıfının **beşinci** vakası) · doğrulayıcı göz **57 vektörle** yendi ve `Mc`'yi dışarıda bırakan gerekçeyi (*"boşluk kaplar → saklanma yeri değil"*) **kendi kanıtıyla çürüttü**: `U+3164` HANGUL FILLER kategori **`Lo`** ve **sıfır genişlikte** render oluyor → ***"sınır ilkeli değil, aramanın durduğu yer"*** · **YANLIŞ ALARM MALİYETİ ÖLÇÜLDÜ, İDDİA EDİLMEDİ: 662 873 gerçek ad** — geniş küme dar kümenin kaçırdığı **tam olarak aynı 74 hücreyi** kaçırıyor (**delta 0**), 30 çok dilli elle kurulmuş ad → **0 değişim** · ⚠️ **SÖMÜRÜNÜN YARISI ÖLÇÜLEMEZ** (hesap tablosu motoru yok, **üç denetim de aynı duvara çarptı**) — *"hücre kaçışsız iniyor"* ölçüldü, *"formül çalışıyor"* **iddia edilmiyor**; açık sınıflar **koda sayıldı** · **kararlar:** **GET** (indirme bağlantı olarak yaşamalı; `sameOriginGate` **yer iminden açılan** indirmeyi reddeder — yapıcı *"eski tarayıcılar"* demişti, denetçi **her tarayıcıda** olduğunu ölçtü) · **UTF-8 BOM VAR** (BOM'suz UTF-8 Excel'de `ħ ġ ċ ż` / `ı ş ğ ç` bozuyor — **ürünün iki pazarı**; azaltma: **ilk satır bir başlık CÜMLESİ**) · audit **`Record`**, `RecordTx` değil (`Hours` bir **okuma**) · **dosya adında tenant adı YOK** (asıl risk Go'nun CR/LF'i boşluğa çevirip **tırnağı çevirmemesi**; regexp **gerçek bir girdide ateşledi**) · **`report:export` motora BAĞLANMADI ve bu RAPORA DEĞİL KODA yazıldı** (gerçek kapı **M6-09**) · **⚠️ İKİ DENETÇİ BİR SAYIDA ÇELİŞTİ VE YAPICI ÇÖZDÜ** (imza dersinin **denetçide** tekrarı): genel göz *"eşiğe **869 satır**"* dedi (ham `type IN ('in','out')`), güvenlik merceği sorgunun **gerçekten okuduğu pencereyi** ölçtü; dört varyant **bağımsız SQL'le** üretildi — **%96 · %111 · %54 · %63 (BU SORGU)**, **farkın tamamı `NOT t.practice`**; orkestratörün `1,64×`'i **yanlış değil BAYAT** → **1,58×** · **güvenlik merceği §4.2'yi YAPISAL olarak kapattı** (`ledger` tip grafının **tüm alanları** döküldü — `Trust`/`IPMatch`/`GPS*`/`SourceIP`/`Distance`/`PolicyContext`/`EnteredBy`/`Note` **hiçbiri yok** → türetilmiş sızıntı **imkânsız**), `?week=` ile **dokuz** düşmanca deneme hiçbiri `Content-Disposition`'a ulaşamadı, anonim indirme **audit satırı yazmıyor**, çapraz-origin **gövdeyi okuyamıyor** · **yapıcı kendi hatasını DOKUZ kez bildirdi** — en öğreticisi **iki kez aynı sınıf**: parite ve needs-action fixture'ları **ayırt edici değildi**, bu yüzden mutasyonlar **yeşil kalıyordu**; ve bir harness **kodu değil YORUMU** mutasyona uğratıp `git diff` boş olmadığı için "uygulandı" göründü · **2493 PASS / 0 FAIL / 0 SKIP** ‖ **A: `671289b`** · **ÜÇ denetçi ONAY** (genel üçüncü göz · `tappa-security-auditor` · düzeltme turunu doğrulayan üçüncü göz) · **4 tur, 0 RED — projenin İLK sıfır-RED görevi** · **yeni migration YOK** (son 00013) · görev **denetim merceğine göre** A/B bölündü (A = aritmetik/§4.5/§4.6/§4.7 · B = çıktı yüzeyi/enjeksiyon/toplu veri çıkışı), **B sekiz yükümlülük** miras alıyor · **saat motoru `internal/domain/ledger/report.go`**, iki yeni sorgu (`ListWorkedShiftEvents`, `CountPracticeTaps`) · **🔴 TEK DAVRANIŞ DEĞİŞİKLİĞİ FAIL-SAFE:** `endpointState`'in `default`'u **bilinmeyen her verdict'i ödenebilir** sayıyordu (`("void","")` = `counted`, reddedilmiş çift → `Worked=8h`) ve savunan yorum **şemada olmayan** bir CHECK'e dayanıyordu — gerçekte tek kısıt **tek yönlü**, rollback'li sonda `verdict='reject', type='in'`'i **kabul ettirdi**; ulaşan yol **yok** (dört yol kapalı, canlı **303.128 satırda 0 aykırı**) ama bariyer **kod değişmezi**, ve **M6-08 ikinci yazıcıyı ekliyor** → `default` **`HoursAwaiting`** (§4.6: kaybolmaz **ve** sessiz onay yok); **hiçbir gerçek sayının değişmediği ölçüldü** — 3 tenant × 4 hafta, **12/12 bayt bayt aynı** · **🔴 DEVRALDIĞIMIZ BORÇ KAPANDI:** `tap.Decide`'ın `MinutesLate`'i **sunucu saatini** ölçüyordu (geriye tarihli giriş **−520**); kapı ölçümü — `minutes_late` **sütunu yok**, template yok, log yok, `CtxTimeMinutesLate` **`keys` map'inde hiç set edilmiyor**, ve lateness **policy'den sonra** hesaplanıyor (`:212-230`→`:256`) → hiçbir guardrail'e giremez; düzeltildi, rapor `occurred_at`'ten **yeniden hesaplıyor** (§4.3), ve `want 17` testi **zayıflatmadı SIKILAŞTIRDI** (`abs>2` toleransı → **tam eşitlik**; yorum HEAD'de **zaten vardı**) · **⚠️ İMZA SINIFI ON KEZ: bir ağın yakaladığı ile yakaladığını SÖYLEDİĞİ ayrı** — üründe **tek sızıntı yok**, yanlış olan **yazılı kapsam**: **§6 float ağı** yalnız `float64` **kelimesini** arıyordu ve gerçek akümülatör (`ph.Worked.Seconds()*1e9 + …`) **tüm paketi yeşil** bıraktı → erişimciler + `token.FLOAT` eklendi, **kalan sınır `go/types` ister ve SAYILDI** (iki kaçış kuruldu, ikisi de yeşil) · **§4.7 refleksiyon testi** tam eşleşmeli **dokuz ad**tı → `Coords`/`GPS`/`SessionToken`/`InviteCode` geçiyordu; düzeltme sonrası ikinci denetçi **`RemoteAddr`**'ı geçirdi (**Go'nun `source_ip` için kendi alan adı** — `address` vardı, **`addr` yoktu**) → alt dize + **CamelCase kelime-tam** iki mod, yanlış alarm **ölçülerek** (53 alan; tek çakışma `Late`↔`lat`; `Resource*` re-**source**'a takılıyordu; **`position` bilinçli yasaklanmadı** — repoda **82 meşru kullanım**, keyset cursor) · **satır tavanları** (`maxReportRows`/`maxOpenRows` 100, `ReportEventCap` 20 000) **üçü de ekranda ilan ediliyor** ve `Truncated` değişmezi `readLimit()`/`truncatedBy()` olarak çıkarılıp **iki mutasyonla** pinlendi (`>`→`>=` ve `readLimit()`→`Cap`: *"Truncated is dead code"*) · **⚠️ ÜÇÜNCÜ KEZ AYNI EXPLAIN, ÜÇÜNCÜ KEZ FARKLI İNDEKS** — ikisi ertesi gün, üçüncüsü **bağımsız bir okuyucu tarafından AYNI GÜN** çürütüldü → **indeks adı bloktan tamamen silindi**, kalan şey plan **şekli** + büyüklükler + tarih · **kararlar (otonomi, hepsi ölçümle):** gece vardiyası **girişin gününe** sayılır (bölmek 02:00 çıkışının 2 saatini mekânın **kapalı olduğu** güne yazar ve **geri döndürülemez**; ekran *"counts on the day it STARTED"* diyor) · `ok` + **onaylanmış** flag sayılır, **bekleyen ayrı** (%90,9 = 25 550 satır, saymak bordroya kimse bakmadan sokardı), **reddedilmiş ayrı** · hafta **pazartesi** (ISO 8601), gün sınırı **yerel** (DST 167/169h testli) · `report:export` policy motoruna **bağlanmadı** (panelde `Evaluate` çağıran handler **yok**; rol kapısı baseline gereği **kimseyi reddetmezdi** → B `audit_log` yazacak, gerçek kapı M6-09) · **güvenlik merceği:** dört çapraz-tenant sondası **pozitif kontrollü** (A/A **14 153** · B id'siyle 0 · yüklemsiz 0 · JOIN'den isim 0), isim sızıntısı **kurularak denendi** ve composite FK reddetti, `AdvanceTagCounter` **dokunulmamış** (`git diff` boş), `endpointState("ok","rejected")` **erişilemez** (canlı: flag'e bağlı review **18 460**, non-flag'e **0**) · **⚠️ YAPICI KENDİ HATASINI ON İKİ KEZ BİLDİRDİ** — en öğreticisi: bir mutasyonun *"ağı yenemediğini"* raporlayacaktı, oysa python geri-yükleme betiği yalnız **yorumu** geri koymuştu ve **ağ hiç kurulmamıştı**; kendi debug probe'uyla yakaladı · **2416 PASS / 0 FAIL / 0 SKIP** |
| M6-08 | Manuel kayıt girişi | **done** | **`03a95fa` + MIGRATION 00014** · **ÜÇ denetçi ONAY + BİR RED** · **5 tur** · **ürünün `transactions`'a İKİNCİ YAZICISI** (`internal/domain/manual/`) · **Q18 burada kapanıyor**: sistem çıkış üretmez, M6-07'nin listelediği açık kayıtları **bir insan** kapatır · `channel='manual'` · `entered_by` **oturumdan** · **`sun_valid=NULL`** (kart `false` diyordu, **yanlıştı** — sütun üç değerli) · trust **taban** · `verdict='ok'` (`flag` olsaydı saat **`HoursAwaiting`**'e düşerdi, müdür açık kaydı kapatırdı ve **toplam yine eksik kalırdı** — Q18'in tersi) · **🔴 GÖREVİN DÜRÜST YARISI ÖLÇÜMLE ÇIKTI:** eşleme motoru **en geç `in` + en erken `out`** alıyor → eklenen satır **yalnızca KISALTIR**, yani **az ödemeyi onaran iki yön çalışmıyor**; ve bir `in` düzeltmesi **KAPATILAMAZ bir açık kayıt** bırakıyor (**iki denetçi bağımsız üretti**: 7h / open=1 / startedEarlier **1→2→3**), müdür ekranda **iki kere yanlış** bir cümle okuyor. **Davranış DEĞİŞTİRİLMEDİ** (motor M6-07'nin sevk edilmişi) — ölçüldü, **onay ekranına** yazıldı, **[ADR 0011](../adr/0011-duzeltme-satiri-yalnizca-kisaltir.md)** üç çıkış yoluyla kaydedildi (ADR 0009'un kardeşi, bu kez **parasal**); ✅ pozitif kontrol: **asıl senaryo (unutulmuş çıkış) DOĞRU çalışıyor** (`in09` → open=1; +`out17` → **8h, open=0**) · **🔴 MIGRATION 00014 BİR BARİYERİ YAPISAL YAPTI:** şemada *"ok ⟹ yön"* vardı, **tersi yoktu** → `verdict='reject', type='in'` **kabul ediliyordu** (üç denetçi de rollback'li sondayla kabul ettirdi); artık `CHECK (verdict IN ('ok','flag') OR type IS NULL)` **VALIDATED**, **0 ihlal / 333 481 satır**, `Down` **güvenlik geriye-gidişi olarak adıyla** yazılı · **pozitif biçim bilinçli — ve gerekçesi BİR KEZ YANLIŞ YAZILDI, denetçi çürüttü** (*"`NOT IN` nullable sütunda sessizce bozulur"* → ölçüldü, **iki biçim de aynı** bozuluyor; **gerçek** fayda: sözlüğe **yeni bir verdict** girerse `NOT IN` yönlü `'void'`'i **KABUL** eder, `IN` **REDDEDER**) · **⚠️ ORKESTRATÖRÜN UYARISI İYİ YÖNDE ÇÜRÜTÜLDÜ:** *"böyle bir satır rapora çalışılmış saat girer"* **yanlış** — **M6-07 A'nın `endpointState` fail-safe'i zaten karantinaya alıyor** (`reject`+yön → `worked=0 / awaiting=8h`); bariyer **ikiden dörde** çıktı ve `decide.go:239`'un `if`'i **çıplak invariant değil** (`TestDecide_DirectionNilForNonRecordVerdicts`, mutasyon **dört alt vakada** kırmızı) · **kararlar:** policy motoru **bağlanmadı** ve gerekçe **koda** yazıldı (panelde `Evaluate` yok · baseline **iki role de** veriyor · **ve `forTenant` baseline'ı MATERIALISE ediyor** → bir panel POST'u policy tablolarına **yazardı**), guardrail'in ***"authorized"*** kelimesi **silindi** · onay kapısı **gerekli** · reddedilen yazma **200 + yeniden render** (not/tarih/saat/yön korunuyor; **çift yazma kurulup denendi, yazdırılamadı**) · `manager_entered_shifts` **iki yönde de yanlıştı** → aritmetiğe dokunulmadı, **ad ölçüme eşitlendi** (`manager_entered_arrivals`) · **🔴 `make test` DETERMİNİSTİK YAPILDI** (kapsam genişlemesi): `TestPlaqueJourneyDB_...` **kendi tenant'ını zehirliyordu** (koşum başına 2 plaket, kapaklı liste, tenant **287**, **P(fail) ≈ %28,8 ve artıyor**) → **8/8** tek-başına koşum, delta **+16 → 0**; `vat_number`'dan sonra bu sınıfın **ikinci** vakası · **⚠️ TEK RED, SINIF TANIDIK: düzeltme turu kendi değişikliklerinin TERSİNİ söyleyen DÖRT metin bıraktı** (`checkin.go` *"veritabanı kabul eder"* + **var olmayan bir teste atıf** · `manualentry_db_test.go` aynısı, üstelik 00014'ün **kendi başlığının FALSE ilan ettiği** cümleyle · `manualentry.go` *"her sonuç 303"* → **beş dal 200** · migration gerekçesi) — **dördü de kapatıldı** · **denetçi türetilmiş §4.5 ağını İKİ bağımsız yoldan yendi** (INSERT'ün **yazdığı** `tenant_id`'ye hiç bakmıyor · yalnız işaret edilen dosyaları kapsıyor) ve **kör noktayı ürün genelinde 7 sorgu** saydı → **backlog T20** · **yapıcı kendi hatasını SEKİZ kez bildirdi**, biri ciddi: **harness'ı `git checkout` kullandı ve `InsertManualTransaction`'ı SİLDİ** (üretilen store dosyasından kurtarıldı) · **2597 PASS / 0 FAIL / 0 SKIP · 17 paket** |
| M6-09 | Policy yönetim ekranı | **done (A+B)** | **B: `2cf55f2` + MIGRATION 00015** · **11 TUR, 10 RED — projenin en uzun ve en çok RED alan görevi** (her turda **YENİ** üçüncü göz — on farklı denetçi — + `tappa-security-auditor` **iki kez, ikisi de ONAY**) · **🔴 SEVK EDİLEN ÜRÜNE TEK KUSUR GEÇMEDİ**: on turun bulduğu her şey (düşen ad · kırık `make check` · checkbox dokunma hedefi) commit'ten **önce** yakalandı, ve **son iki tur 0 ürün / 0 build** çıkardı → **birinci durma kuralı** · **`policy:edit` ÜÇ GÖREV ERTELENDİKTEN SONRA GERÇEKTEN ZORLANDI** ve yol **ölçerek** seçildi: **(b)** `checkin.Service.StoredSet` = `forTenant`'ın **materialise ETMEYEN** hâli — **(a)** elendi çünkü reddedilen bir manager POST'u append-only tablolara **9/9/9** yazardı, **(c)** elendi çünkü kapıyı `if actor.Role=="owner"` yapmak **iki testi kırmızı** ediyor (motordan farklı cevap) · ⚠️ **naif kapı SAHİBİ KENDİ EKRANINDAN KİLİTLERDİ** — guardrail `sys:policy-edit-owner-only` yalnız **non-owner**'ı reddeder, owner'ın izni **baseline belgesinde**, yani boş `Set` ile owner da `deny` (`baseline.go:212-221` bunu kendi yazıyor) · **🔴 MIGRATION 00015 — BİR KİLİT SANILAN ŞEY BİR BAKIŞTI:** `PolicyNameTaken` klasik **oku-sonra-yaz** idi (§4.4'ün dersi, bu kez bedeli **geri alınamaz satır**: `policies` DELETE vermiyor) ve **eşzamanlı bağlantı başına bir ikiz** üretiyordu (`racers=64 → 16 satır`, `NumCPU=16`; TOCTOU birebir görüntülendi); `UNIQUE (tenant_id, layer, name)` **VALIDATED**, kapsam **ölçerek** seçildi (`layer` dahil — katı biçim `TestPolicies_LayerCheckRejectsGuardrail`'in **§4 pozitif kontrolünü** kırardı), ve **indeksin taşıyıcı olduğu ayrı ayrı kanıtlandı**: guard **köreltilince** 64 racer **hâlâ 1 satır**, 63 reddin hepsi **23505'ten** · **⚠️ AĞ FAZ B'DE ALTI KEZ DAHA YENİLDİ (toplam 13) VE ÜRÜN ON ÜÇÜNDE DE TEMİZDİ** — kırılan hep **koruma**: bölüm-dışı · **dolu dekoy sarmalayıcı** (`strings.Index` **ilk** eşleşmeyi alıyordu) · **HTML yorumu** içindeki `</div>` · **HTML5 bogus comment** `<!x … >` (tarayıcı yutuyor, sayaç **sayıyor**; `<!DOCTYPE`/`<?x>`/`<![CDATA[` kardeşleriyle) · **çağrının yanına** konan kontrol · press-target sözlüğü **olmadan** buton görünümü · **sunucunun okumadığı alan adı** · **action'sız form** → **kapatanlar hep TÜRETİM oldu**: alan adları **`go/ast`** ile, ve sonunda **sayaç tamamen kaldırılıp bölge `PolicyGuarantees`'in İZOLE RENDER'ından alındı** (faz A'nın bayt-tam eşitlik tekniği bir kat aşağıda; sarmalayıcı bileşenin **tek kök elemanı** olunca aralarına **hiçbir şey** giremiyor) · **kalanlar SAYILDI** (sevk edilen switch formuyla **şekil olarak birebir aynı** kontrol — on üçüncü yama tam oraya düşerdi; alansız bağlantı; `templ.Raw`/`switch` kör noktası; ödünç tanık) · **🔴 VE AĞIN DUVAR OLMADIĞI KAYDA GEÇTİ:** kapatılamazlık **şemanın ve motorun** işi — `layer='guardrail'` INSERT'i **superuser'a bile** CHECK'e takılıyor, ve **en özgül + koşulsuz + izin veren** bir tenant kopyası karşısında motor **yine** `deny/sys:*` veriyor (güvenlik merceği ölçtü); ağ bir **regresyon freni** · **⚠️ İMZA KUSURU (*"sağlamadığı garantiyi beyan etmek"*) FAZ B'DE ON BEŞTEN FAZLA KEZ ÇIKTI ve altı turun beşinde bloklayan oradan geldi** — en öğreticileri: **7. turun DOĞRU düzeltmesi** (fixture'ın dokuz belgeyi de seed etmesi) kartın *"ölçüldü"* diye gösterdiği **falsifier'ı sessizce öldürdü** (*bir düzeltme başka yerdeki bir ölçümü geçersiz kılar* — yeni sınıf) · onay ekranı **00008'in CHECK'inin yasakladığı** bir garanti basıyordu (*"her kayıt onu yargılayan sürümü adlandırır"* — guardrail kararları `policy_version_id IS NULL` **şart**) ve düzeltmesi **dört kolun üçünü** okuyup dördüncüyü atladı: **manuel kayıt yolu** (M6-08, bir görev önce sevk edilmiş) `policy_layer IS NULL` üretiyor → **veritabanının %47,8'i hiçbir kural adlandırmıyor** · bir bildirim **başlığı** kendi **gövdesinin tersini** söylüyordu (`lockout-stands`), ve o kusur için kurulan koruma **başlığı hiç okumuyordu** · *"her red audit'e yazılır"* düzeltilince yerine **eksiltici** ama yine kategorik *"diğerleri yazılmaz"* kondu — ölçüm: lockout reddi **iki** satır yazıyor ve satırı **gerçekten değiştirip geri koyuyor** · **🔴 `make check` SEKİZ TUR BOYUNCA YANLIŞ RAPORLANDI** — `make test` yeşil olduğu için yeşil sanıldı; gerçek: **`templ fmt` `policies.templ`'i değiştiriyordu** (`changed=1`), yani `check` **kaynağı kirletip** `git diff --exit-code`'a takılırdı (orkestratör ölçtü); artık **manifest önce == sonra** ile kanıtlanıyor ve `Makefile`'ın *"bugün hiçbir `.templ`'i değiştirmiyor"* gözlemi **bayatlamayan** bir iddiayla değişti · **iki TEST DOUBLE üretimden AYRIŞIYORDU ve tam da sınandıkları özellikte** (`storedAuthority` hep-ya-hiç kuralını uygulamıyordu · `fakeScribe` yazma metotlarında `may`'i **hiç okumuyordu** → apply adımının red kolu **ölü kod**) — ikisi de **sözleşmeye eşitlendi**, mutasyonla pinlendi · **karşılanmayan DÖRT kabul kriteri de kartta yazılı** (davranış seçen form · departman kapsamı · aralık dışı değerin arayüzde reddi · *"ne değiştirdi"* diff'i) — *"karşılanmadı ama karşılandı denmiş"* **yok** · **güvenlik merceği (2. kez, sıfırdan): §4'ün yedisi temiz** — 11 yeni sorgunun **11'i** §4.5 ağında (kör **0**), RLS beş yolda kapalı, kompozit FK çapraz-tenant `policy_id`'yi kesiyor, 00015 **varlık oracle'ı değil**, `GET …/version` **ham jsonb basmıyor**, MAC payload'ında **oturum satır id'si** (token değil), tip grafında **tek koordinat alanı yok** · **yapıcı kendi hatasını ~30 kez bildirdi**, en değerlisi *"beklenen kırmızının gelmemesi bir bulgudur"* refleksiyle üç kez sessiz bir edit hatasını yakalaması · **2710 PASS / 0 FAIL / 0 SKIP · 17 paket** ‖ **A: `6738687`** · **8 TUR, 4 RED** (üç genel üçüncü göz + `tappa-security-auditor` + kapanış denetçisi) · **yeni migration YOK** · görev **denetim merceğine göre** bölündü (A = okuma · B = yazma), **B on yükümlülük** miras alıyor · **🔴 GUARDRAIL KAPATILAMAZLIĞI ÜÇ DUVARLA YAPISAL** (domain tipi **kontrol alanı taşımıyor** · view tipi **yalnız string** · ağ şablonun **her dalını** render edip inert olmayan her şeyi kırmızıya çeviriyor), **sıra `policy.Guardrails()`'ten TÜRETİLİYOR**, ve güvenlik merceği **taklidi kurarak** denedi: `sys:`/`base:` sid + `ignore`/`redirect` → **dördü de `StateUnreadable`, 0 statement, guardrail bölümü değişmiyor** · **🔴 OKUMA HİÇBİR ŞEY YAZMIYOR** (statik **ve** canlı: baseline'sız **0/0/0→0/0/0**, sağlıklı **9/9/9→9/9/9**, bozuk **2/2/2→2/2/2**) — `checkin.forTenant` materialise ediyor, panel o yolu **kullanmıyor** · **🔴 BLOKLAYAN BULGU EKRANIN VARLIK SEBEBİNE DOKUNDU:** dokuz baseline belgesinden **biri** okunamazsa motor guardrail'lere düşüyor ama ekran diğer **sekizini `In force`** ve onlardan türeyen **`Granted to`** izinlerini basıyordu — **ADR 0004'ün *"var olan en tehlikeli hata"***'sı, ve **yapıcının kendi yorumu bunu adıyla koymuştu** (kusuru o kural için çözüp **yayılım alanı için** çözmemişti); bayrak **türetildi** (hangi durumun uyarıyı hak ettiği ölçülerek: `StateOff` **atlanıyor**, iki durum **kendini onarıyor**, `StateUnreadable` **onarılamıyor**), sonra **kapalı+okunamayan** yanlış alarmı için `enabled` kontrolü **parse'tan öne** alındı — panel motorun ayrımını **taklit etmiyor, tekrarlıyor** (dokuz belgelik matriste **ayrışan girdi yok**) · **⚠️ İNERT-MARKUP AĞI YEDİ KEZ YENİLDİ, ÜRÜN YEDİSİNDE DE TEMİZ** (kırılan hep **koruma**): deny-list dardı · **düzeltmenin kendisi** ağın göremediği yere düştü · **dört koşullu bloğu hiçbir fixture sürmüyordu** (biri üretimde **her sayfada**, *"no policy can widen this one"* cümlesinin dibinde) · **kabuğun kendi token sözlüğüyle** kurulan tam işlevli POST kontrolü · **ödünç tanık** · `switch`/düz-Go · ve **öznitelik değerindeki ham `>`** tarayıcıyı körleştiriyordu → **ÜÇ TÜRETİMLE** kapatıldı (fixture kümesi **`.templ`'den** — ilk koşu **sekiz sürülmeyen dal + altı ayırt etmeyen tanık** buldu · token listesi yerine **bayt-tam eşitlik** · tarayıcı okuyamadığını **reddediyor**), kalanlar **ikinci durma kuralıyla SAYILDI** (*"NE TUTAR / NE TUTMAZ"* envanteri) · **⚠️ *"sağlamadığı garantiyi beyan etme"* sınıfı DÖRT kez** — sonuncusu: bir başlık *"her dalı TÜRETİR"* diyordu ve **iki satır altta** *"HER DALI TÜRETMİYOR, VE BAŞLIK ÖYLE DİYORDU"* yazıyordu, **başlık değişmemişti**; yapıcı kendi limit cümlesini **iki kez olduğundan küçük** yazdı ve ikincisinde **türeterek** kapattı · **güvenlik merceği: §4'ün yedisi de temiz**, kontrast **13,27/16,17/10,16/6,05/9,13** hepsi AA · **2657 PASS / 0 FAIL / 0 SKIP · 17 paket** |
| M6-10 | Policy simülatörü | skipped | Q22 → M9-06'ya ertelendi |
| M6-11 | Anomali ve kötüye kullanım raporu | **done** | **`f39a341`** · **3 TUR, 2 RED** (üç **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**) · **yeni migration YOK** · `/admin/anomalies`, **8 salt-okuma sorgusu** · **🔴 SEKİZ KRİTERİN İKİSİ KAYNAKSIZ ÇIKTI VE UYDURULMADI, DÜŞÜRÜLDÜ — ikisi de ölçüldü, ikisi de EKRANDA ilan ediliyor:** *"POST'suz `GET /t`"* (kart biliyordu — `tap_page_views` **kullanıcı kararıyla yapılmadı**, `GET /t` **stateless**, oturumsuz istek **303'te duruyor**) · ve **orkestratörün kart ölçümünde bulduğu ikincisi**: *"tek cihaz/oturumdan çoklu aktivasyon"* — `sessions`'ta `created_ip` **yok**, eldeki `device_info` **7.430 oturumda 25 farklı değer** (⅕'i NULL), *"aynı cihaz + çok çalışan"* **273 grup** = her cihaz ailesi · **ve yapıcının bulduğu üçüncüsü:** *"hiç çapraz-lokasyon göstermeyen çalışan"* **974 çalışanın 974'ünü** işaretliyor (iki bağımsız ölçüm, `ever_cross=0`) → kriter anlamsız, yerine **çapraz-lokasyon sayısı** · **🔴 KART MOTORUN MEKANİZMASINI YANLIŞ ANLATIYORDU:** Y-E'yi *"**IP eşleşti ama** GPS uyuşmuyor"* diye tanımlıyordu; `decide.go:715-722` `conflict = !match` döndürüyor — **yalnız mesafeden**, adrese **hiç bakmadan** — ve `baseline.go:206-208` koşulu yalnız `{CtxTapGPSConflict: true}`, yani **politika da daraltmıyor**; kart `decide.go` alıntısıyla düzeltildi, ekran **bayrağı bayrak** olarak basıp *"bunların kaçı IP'yle de eşleşti"*yi **ayrı sayı** veriyor · **🔴 BLOKLAYAN BULGU GERÇEK ARİTMETİKTİ:** kod **iki yerde** *"snapshot'sız kayıt temiz SAYILMAZ"* ilan ederken snapshot'tan türeyen üç oranın **paydası onları içeriyordu** — üstelik **iki farklı payda** (`Records` ve `Judged`) — ve canlı sayfada `39 of 2100 · 1%` satırının **hemen altında** *"173 kayıt snapshot taşımıyor, temiz sayılmıyorlar"* yazıyordu; düzeltme `Answerable = Records − Unanswerable` ve **hangi sayının hangi tabandan** olduğunu söyleyen `BasisLine` **her zaman** basılıyor, fixture **22/20/18 üç farklı sayı** olacak şekilde yeniden kuruldu, test **hem bekliyor hem adıyla reddediyor** · **⚠️ HIZ İDDİASI ÜÇ KEZ ÖLÇÜLDÜ, ÜÇÜ DE AYRIŞTI, VE SONUNDA TAMAMEN GERİ ÇEKİLDİ** (yapıcı 209/221/252 ms → denetçi **17–19 ms** → yapıcı yeniden **103–110** → kapanış denetçisi **276–290**); sebep ölçüldü: **üç farklı join stratejisi** (Hash · nested loop · **Merge**) ve büyüyen pencere (1.063 → 1.365 → 1.547 → **1.657**, çünkü her `make test` içine yazıyor ve §4.3 geri almayı yasaklıyor). Yerine **makineden bağımsız kesir**: `1.560.627 / 1.863.225 ≈ %84` kartezyen — kapanış denetçisi **farklı makinede, farklı stratejiyle, farklı pencerede %83,9** ölçtü. **`lag()` kararı doğru ve düşünülenden güçlü** (~45×) · **🔴 §4.7 DESEN AĞI COĞRAFYA KÖRÜYDÜ:** `\b\d{2,3}\.\d{2,}\b` **iki tam haneden az hiçbir dereceyi göremiyor** — 0°–10° arası her enlem/boylam (Accra · Singapur · Londra · Paris), **Malta desenin çalıştığı tek coğrafya**; denetçi uçtan uca kanıtladı (`(gps_lat-30)::text` → sayfada `5.898909`, **bütün duvarlar yeşil**). Genişletme **önce ölçüldü** (üç sayfa şeklinde ondalık-şekilli dizgi **0/0/0** — sayfa hiç ondalık basmıyor) → **bedel sıfır** → `\b\d{1,3}[.,]\d{2,}\b`; kalan iki sınıf (**bilimsel gösterim** — tam precision enlemi yeşil geçirdi — ve **ASCII dışı ayırıcılar**) **sayıldı**, ve saymanın gerekçesi **maliyet değil kapsam** olarak yazıldı (genişletme de sıfıra mal olurdu; ama sevk edilen hiçbir sorgu koordinat seçmiyor) · **güvenlik merceği ONAY:** 8 sorgunun 8'i §4.5 kemerinde, **hiçbiri** `gps_lat`/`gps_lng`/`source_ip` seçmiyor (tek eşleşme bir **yorum**), `policy_context` **sınırı geçmiyor**, RLS beş yolda kapalı — ve iki ağırlık kararı: §4.2 *"hareket örüntüsü"* **sınır içinde** (ekran hiçbir şey **toplamıyor**, transactions bölümünden **daha az** gösteriyor), ama `ListTapsTakenTogether` **yeni bir sosyal çıkarım yeteneği** → backlog **T25** · **test double üretimden ayrışıyordu ve sebebi öğreticiydi:** fixture her `sid`'i `guardrail` damgalıyordu çünkü **şema CHECK'i** (`transactions_policy_decision_consistent`) baseline katmanında `policy_version_id` **şart koşuyor** — yani ikiz, şemayı atlatmanın **kolay yolunu** seçmişti; artık gerçek `policies`+`policy_versions` satırı yazıyor · **paylaşılan `readLimit(cap int32)` M6-07 ile ortaklaştırıldı**, semantik **birebir** korundu ve mutasyon **iki bölümü birden** kırmızı yapıyor · **yapıcı kendi hatasını ON DÖRT kez bildirdi**, en öğreticisi üç kez tekrarlayan aynı desen: *"bir denetçinin adını koyduğu kusuru düzeltip **kardeşlerini aramamak**"* · **2739 PASS / 0 FAIL / 0 SKIP · 17 paket** |
| M6-12 | Çalışan sayımı ve fatura taslağı | **done (A+B)** | **B: `5b2736a`** · **3 TUR, 0 RED** (üçüncü göz **1. turda ONAY** — projede nadir — + `tappa-security-auditor` **ONAY**) · `/admin/billing` **sekizinci bölüm** + CSV + founding uyarısı + iki POST · **🔴 ROL KAPISI POLICY MOTORUNA BAĞLANMADI VE GEREKÇE ORKESTRATÖRÜN ÇERÇEVESİNİ ÇÜRÜTTÜ:** brief *"(a) motora bağla, M6-09 B'nin yolu"* diyordu; yapıcı üç ölçümle karşı çıktı — sözlük **kapalı** (`unknown action "billing:close"`, ADR ister) · **DB'de SAKLI** baseline'a karşı bir **owner** `Evaluate` edilince **`deny sid=default`** (saklı `base:authz-owner` **tam altı** eylem taşıyor, `baseline.go` var olan tenant'ları **güncellemiyor**) yani kapı **sahibi kilitlerdi** · ve `billing:close` için **guardrail YOK**, izin `base:authz-*`'da olsaydı tenant **ezebilirdi** — denetçi bunu **kanıtladı**: tenant kuralı koyunca `allow sid=tenant:let-managers-close`, yani **Tappa'nın kendi faturalama kapısı müşterinin kontrolüne girerdi**; M6-09'un rol-kontrolü itirazı buraya **ulaşmıyor** çünkü orada motor **farklı ve DOĞRU** cevap veriyor (`sid=sys:policy-edit-owner-only`), burada motorun **hiçbir görüşü yok** · **🔴 BÖLÜM OWNER-ONLY, OKUMA DAHİL (orkestratör kararı):** güvenlik merceği bir **manager**'ın işletmenin Tappa'ya borcunu okuyabildiğini ölçtü — §4 ihlali değil ama panelin hiç göstermediği ticari bilgi; Faz A `plan`/`price`/`created_at`'i uygulama rolüne **hem INSERT hem UPDATE**'te kapatmışken okumayı açık bırakmak duruşu yarım bırakıyordu · ⚠️ **filtre NAVİGASYONA kondu, ROTA TABLOSUNA değil** — `mountSections` hâlâ **tam listeyi** geziyor, yani manager **handler'dan 403** alıyor, chi'dan 404 değil, ve *"linki 404 veren sekme"* **yapısal olarak imkânsız** kalıyor; filtreyi rota tablosuna taşıyan mutasyon **dört testi** kırmızı ediyor · **iki yeni audit eylemi (ikisi de orkestratör kararı):** reddedilen kapatma **her iki adımda** `billing.period_close_refused` (emsalin gerekçesi birebir uyuyor: *"log satırının operatör dışında okuru yok; bunu en çok bilmesi gereken kişi SAHİPTİR"*, ve `location.delete_refused` bir **kullanıcı kararı**) · CSV `billing.exported` (`before=743 · fatura sonrası 743 · rapor sonrası 744` ölçüldü; sebep gizlilik değil **tutarlılık** — iki kardeş rota aynı repoda farklı cevap veremez) · **CSV kaçışı `reportscsv.go`'dan TAMAMEN devralındı** (`billingcsv.go`'da **hiç kaçış kodu yok**, her hücre `reportDoc.row` → `spreadsheetSafe`; bypass mutasyonu **7 alt test** kırmızı) · **PDF TESLİM EDİLMEDİ** — repoda üreteç yok, §1 Node'u yasaklıyor ⇒ yeni Go bağımlılığı ⇒ **kullanıcıya sorulur**, kart düzeltildi · **güvenlik merceği sertçe ölçtü:** çapraz tenant onay token'ı **kırılmıyor** (oturum imzanın içinde; KF token'ı KM oturumunda → `confirm-required`, 0 satır) · tenant **altı yoldan** enjekte edilemiyor · rol **altı yoldan** satın alınamıyor · **KF'nin 2213 farklı çalışan adı** ekrana ve CSV'ye tarandı → **0 eşleşme** (detektör `/admin/employees`'te eşleşiyor) · `exit 0`'lar **sentetik ihlalle** boş olmadıkları kanıtlandı · **kapsam genişlemesi (işaretli):** `<link href>` için CSS-embed testi — `app.css` gitignore'lu ve `go:embed` dosya yokken de derliyor, yani temiz klondan **çıplak `go build`** alan biri **her sayfası stilsiz** ikili sevk ederdi; **M0'dan beri açıktı**, `.underline` görünür kıldı · **2932 PASS / 0 FAIL / 0 SKIP · 18 paket** · **FAZ A:** `e085ae6` · **MIGRATION 00016** · **5 TUR, 3 RED** (iki **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**) · Faz B (ekran · CSV · founding uyarısı · "dönemi kapat" rotası) **kaldı** · **A/B bölünmesi orkestratör kararı** (ölçüt agent-brief: veri katmanı + uygulama katmanı ⇒ farklı denetim mercekleri) · **🔴 ORKESTRATÖRÜN ÖLÇMEDEN YAZDIĞI BİR ATIF ÜÇ YERE İŞLENDİ VE DENETÇİ ÇÜRÜTTÜ:** devir notu *"kirlilik seed'de **ve** test fixture'larında"* diyordu; `test/fixtures/seed.sql:225-227` `deactivated_at`'i statüden **türetiyor** → **seed sıfır çelişen satır üretir**, çelişenlerin **hepsinin adı** `Paul Spiteri [sim …]` ve kaynak `internal/handler/seedflow_db_test.go:413` · sayı **kalıcı değil** (83 → 91 → 95 → 96, koşu başına +1) ve **hiçbir ürün yolu bu satırı yazamaz** (`employees.sql:461` + `invites.sql:241`, iki `COALESCE`) → yüklem korundu ama gerekçe **"düzeltici"den "SAVUNMACI"ya** çevrildi, *"€124,50/ay kalıcı"* cümlesi **kaldırıldı** · **🔴 KABUL KRİTERİNİN İSTEDİĞİ CHECK KISITI ÖLÇÜMLE ELENDİ:** iki `CHECK`'i `NOT VALID` ekleyip **tüm süit** koşturuldu — deaktivasyon yarısı **12/10/15/4**, aktivasyon yarısı **201/185/215/7** (≈17× fark); ⚠️ **ilk ölçüm düşük çıkmıştı çünkü ikisi BİRLİKTE koşturulmuş, kaskad hatalar ucuz yarımı maskelemişti** · ve rakamdan **bağımsız** iki argüman çıktı: CHECK **kuralı kanıtlayan fikstürü** reddederdi (kuralı test edilemez kılar) ve *"invited ama activated_at taşıyan"* satır **ikisinden de sağ çıkıyor** · **🔴 TEMBEL DONDURMA REDDEDİLDİ:** geri alınamaz kararı sayfayı ilk açana (crawler/prefetch/retry) vermek append-only tabloda **kalıcı**, ve panelin üç kez ölçülmüş *"okuma yolu yazmaz"* özelliğini kırardı → açık **"dönemi kapat"** eylemi; üç kapı çağıranın `if`'inde değil **ifadenin içinde** (`to_at <= now()` · `month >= signup_month` · `UNIQUE`), **rakam parametresi YOK** · **🔴 YAPICI BRIEF'TE OLMAYAN BİR ZAYIFLIĞI KENDİ BULDU:** §4.5 ağı **kendi SQL'ini reddetti** (tenant **geçişli** bağlanıyordu); yeniden yazdı, üç mutasyonla kanıtladı, ve denetçi **aritmetiğin değişmediğini** altı ay boyunca birebir (`1590/1590`, `1690/1690`) + **kemerin kozmetik olmadığını** süper kullanıcıyla (kemerli **3**, kemersiz **43**) ölçtü · ⚠️ sapma: kemer **CTE'lerin içinde** (sqlc v1.28 dış `WHERE`'de nitelikli **ve** niteliksiz CTE referansını reddediyor, **dört yazım** denendi) · **🔴 GÜVENLİK MERCEĞİ ORTA BULGU: daraltma UPDATE'i kapatıp INSERT'i AÇIK BIRAKMIŞTI** — `tappa_app` `INSERT INTO tenants (…, plan, price) VALUES (…, 0.00)` yazabiliyordu; bugün ürün yolu yok ama **M7 signup'ın (PUBLIC sihirbaz) ilk kullanacağı yüzey** · ⚠️ **ve yapıcı denetçinin verdiği sütun listesinin EKSİK olduğunu ölçtü:** `id` yoktu, onsuz `DEFAULT gen_random_uuid()` bağlamla asla eşleşmez → **her INSERT `WITH CHECK`'e takılır, signup TAMAMEN kırılırdı** (33 çağrı yeri sayıldı); ayrıca `created_at` de kapatıldı — kendi kayıt tarihini yazabilen tenant **kendi bedava aylarını kaydırır** · dondurma **gerçek** (denetçi fiyatı 9,99 yapıp satırın `1.50` kaldığını **denedi**) · beş fonksiyon `prosecdef=f` + `search_path` sabitli + `PUBLIC EXECUTE` **kapatıldı**, gölgeleme dört yoldan denendi · down paylaşılan `tappa_forbid_mutation()`'ı **düşürmüyor** (rollback'li transaction'da ölçüldü) · **2865 PASS / 0 FAIL / 0 SKIP · 18 paket** |
| M6-13 | Çalışan ekleme | **done** | **`5431bad`** · **9 DENETİM TURU / 6 RED**, dokuz farkli ucuncu goz. 🔴 **CANLI URETIMDE BULUNDU:** kullanici `/signup`'tan kaydolup panele girdi ve *"employee'i nasil ekleyecegim"* diye sordu — **ekleyemiyordu**. Olculdu: `db/queries`'de `INSERT INTO employees` **0**, uretim kodunda `CreateEmployee` **0**, `employees.templ`'de `<form>` **0**; calisan yaratan tek yer `test/fixtures/seed.sql`, yani **urunun disi**. **Roster listeliyor, davet ediyor, deaktive ediyor, tasiyor — hicbir yerde DOGURMUYOR.** Bu, bu projenin imza kusur sinifinin urun tarafindaki hali: **var olmayan bir seyi yoneten bir ekran** (M7-04'teki *"Admin daveti"* ile birebir ayni sekil: baslikta vardi, bes kabul kriterinin hicbirinde yoktu, hic yapilmamisti). **§4.5'in UC KEMERI, her biri BAGIMSIZ olculdu:** ifade mekani tenant'in **ICINDE seciyor** (atamiyor) · bilesik FK'ler **her yuklem silinse bile** yabanci id'yi reddediyor (**23503**, iki FK'de de) · ve RLS **`WITH CHECK`** sahte `tenant_id`'yi reddediyor (**42501**, pozitif kontrolle FK degil RLS oldugu kanitlandi). Capraz-tenant denemeleri **iki roster'i da degistirmiyor**. **Bos render eden adlar reddediliyor** (atanmamis kod noktalari · Hangul dolgu · braille bosluk · bidi override · yalniz birlestirici) — **44 girdi x 2 bicim = 88 vaka**; ve **27 mesru ad** (Maltaca, Arapca, Devanagari birlesigi, emoji ZWJ ailesi, `O'Brien-Smith`, kivrik kesme) **birebir** sakalaniyor. Karar **reddetmek, yeniden yazmak degil**: *"yazilandan baska bir seyi sessizce saklamak, `optionalEmail`'in zaten karsi ciktigi kusurdur."* 🔴 **VE BIR KIRMIZI-CIZGI KAPISINDAKI GERCEK BIR DELIK BULUNDU:** CLAUDE.md §4 *"`make audit` bu maddeleri mekanik olarak tarar"* diyor; **§4.7'nin kisisel-veri yarisi HIC taranmiyordu** ve **uc tarihsel sizinti `redline-check exit 0` veriyordu**. Yeni **R7b** kurali kaynagi okuyor. ⚠️ **Ve kapanirken bugun sevk edilmis kodda ULASILABILIR bir delik buldurdu:** `cmd/tappa/main.go`'daki `slog.Default().Error(...)` — uretim agacinda **tek** cagri-ifadesi logger'i — R7b'ye **gorunmuyordu** (sizinti **exit 0** geciyordu); bir ada baglanarak **sekil ortadan kaldirildi** (ayni sizinti artik **exit 1**). 🔴 **§4.7 AGI UC KEZ EKSEN DEGISTIRDI** — *cagri yerleri* → *fikstur* → *girdi uzayi* — ve her seferinde bicim degistirildi; dorduncusunde orkestrator **yama yerine KARAR** istedi. Tip duzeyi redaksiyon **olculdu ve elendi** (**224+46 cagri yeri**, paket capinda bir donusum → **T51**), kaynak taramasi secildi. **Ders: SURULEN yollar uzerinden hicbir test *"hicbir yerde sizinti yok"*u kanitlayamaz** — bu testin kusuru degil, **testin cinsinin siniri**. **Kapanista: 42 mutasyon, 37 yakalandi**, hayatta kalan besin **besi de olcumle aciklandi** (biri icin `make sqlc`'nin elle duzenlemeyi **bayt-birebir** geri yazdigi olculdu). **Urun davranisinda son DORT denetim turunda sifir bulgu.** `make check` **exit 0** temiz agacta. **Kalan iki pinleme bosluğu + bir metin → T51/T52.** | M6-05 · M6-06 |

### M7 — [Portal & signup](m7-portal.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M7-01 | Landing sayfası | **done** | **`3516f59`** · **5 TUR, 1 RED** (iki **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**) · **ADR 0012** · `/` artık 404 değil — **ürünün kimliksiz ulaşılabilen İLK yüzeyi** · **🔴 1. TUR RED'İ TAM DA BRIEF'İN ETRAFINA KURULDUĞU KUSURDU:** sayfa *"Reports and the monthly headcount split by department"* diyordu — Reports'un üç kırılımı **kişi/mekân/açık kayıt**, `department` `reports.templ`'de **0** kez, billing'in **beş dosyasında da 0** kez; ve bu yasağı **yapıcının kendisi yazmıştı** (`landingview.go:26-29`, üç yasaktan biri *"mount edilmemiş yetenek"*) · **asıl yapısal sebep:** `LandingAudiences`'ı tutan **hiçbir test yoktu** (fiyat, bedava ay, çerez, slogan, tablo **pinliydi**, bu blok değildi) → `Claim` tipi **çapasını adlandırmadan derlenemiyor**, her çapa **ürünü OKUYAN** bir kontrole bağlı (şema bloğu · sqlc satır tipi · policy baseline · domain kaynak sırası), **hiçbir türetim cümle karşılaştırmıyor**; dokuz mutasyonun dokuzu kırmızı, **ikisi derleme hatasıyla** · ⚠️ **VE MEKANİZMANIN SINIRI ÜÇ YERE YAZILDI:** denetçi geçen bir çapa + **yalan** bir cümle enjekte etti, paket **yeşil** — *"kaybolan yeteneği yakalar, HİÇ VAR OLMAMIŞI yakalayamaz; B1 tam olarak böyle sevk edildi"* · **🔴 KARDEŞ TARAMASI KENDİ KARDEŞİNİ BULDU** (kimse adlandırmamıştı): *"a tap at another branch is … **noted**"* — `base:cross-location-note`'un gerekçesi kaydın notuna ancak **o ifade eşleştiğinde** ulaşıyor, `SidIPOrGPSOK` **önce** taranıyor, yani adres eşleşmeli tap **IP ifadesinin** gerekçesini taşıyor → o yarım düşürüldü · **🔴 `redline-check.sh` R1'E MUAFİYET — §4'ü ZORLAYAN MEKANİZMANIN KENDİSİ** (kart **parmak izi karşılaştırma tablosu** istiyor, sayfa §4.1'i **inkâr etmek için adlandırmak** zorunda): güvenlik merceği **ilk hâlini kırdı** (`// PROBE: fingerprint terminal -- keep the webauthn attestation` → sessizce geçiyordu) → **üç koşulla** daraltıldı: **yola sınırlı** · **ifade-kapsamlı** (ifade kaldırılınca başka tetikleyici kalmıyorsa muaf) · ve **her koşuda `[R1 · WARN]` basılıyor** (R5'in ilkesi: *"bir muafiyet görünmez olamaz"*) — orkestratör sondayı **kendi** tekrarladı, artık **exit 1** · ⚠️ **VE MUAFİYETİN YARISI GEREKSİZDİ:** denetçi reponun kendi örneğini buldu (`activate.templ:76` *"no biometric data … no fingerprints"* **eski** süzgeçten geçiyor çünkü aynı satırdaki `no fingerprints` mevcut girdiye uyuyor) → metin o kalıba getirildi, `no biometric` **allowlist'ten kaldırıldı**, ve kamuya açık sayfa §4.1 sözünü artık **çalışanın okuduğu ekranla aynı kelimelerle** veriyor · **ters tuzak bulundu:** `documentHead` **her sayfaya** `noindex, nofollow` gömüyordu — pazarlama sayfası **kimsenin bulamayacağı** sayfa olurdu; `robots` bir değere çevrildi, panel/tap/aktivasyon ve **dört yasal iskelet** `noindex` kaldı (canlı ölçüldü) · **yasal metinler bilinçli olarak YAZILMADI** (uydurmak hukuki olarak yanlış belge üretirdi) — dört rota + iskelet + footer, her sayfa **neyi beklediğini** yazıyor; **çerez bildirimi GERÇEK**: altı satır **çerezi yazan `http.Cookie` literalinden** okunuyor (`SameSite`/`Path`/`HttpOnly`/`Secure` mutasyonlarının dördü de kırmızı) · **güvenlik merceği savunmayı KANITLADI:** `pg_stat_activity` `state_change`'i **700 pazarlama isteğinde kımıldamadı**, tek `/activate` isteğinde kımıldadı; önbellek zehirlenmesi **50 ölçümle** elendi (5 rota × 10 istek biçimi, gövde **ve** başlık hash'i tek değer); 20.000 istek 1,23 s, RSS 22,8 MB sabit · **iptal edilen yükleme artık `Debug`** (200 iptal → 200 ERROR satırıydı; *"yavaş telefondaki ziyaretçi bir arıza raporu değildir"*) · **2960 PASS / 0 FAIL / 0 SKIP · 18 paket** |
| M7-02 | Kayıt sihirbazı ve VAT | **done** | **`9ac3065`** · **MIGRATION 00017** · **ADR 0013** · **8 TUR, 4 RED** — projenin en uzun görevi (üç **farklı** üçüncü göz + `tappa-security-auditor` **iki kez**, ikincisi ONAY) · **Q09 orkestratör kararıyla kapandı** (format zorunlu + VIES **en iyi çaba**; `vat_verified` **NULLABLE** çünkü *"bir kesintiyi `false` kaydetmek bir SUÇLAMADIR"*) · **🔴 SEKİZ AY ÖNCEKİ BİR MIGRATION BU GÖREVİ ADIYLA ÇAĞIRIYORDU:** `00011`'in 4. yükümlülüğü *"bugün sömürülebilir değil, ve sebebi adlandırılmaya değer çünkü SÜRESİ DOLUYOR … **M7-02 tam olarak bunu değiştiriyor**"* diyordu — ve altından **belgelenen DoS'tan DAHA KÖTÜ** bir şey çıktı: `MaxCandidates=8` kapağı + `ORDER BY tenant_id` (yazma zamanına göre **rastgele**) ⇒ 20 ekili satır **ödeyen müşteriyi kendi panelinden kilitliyor**, **beş koşunun üçünde** (down sonrası **beşin dördünde**) · **çözüm 00011'in listesinde YOKTU:** `ORDER BY created_at` — *"saldırganın yapamayacağı tek şey DAHA ÖNCE kaydolmaktır"*; listedeki üçü ADR 0013'te **ölçülüp elendi** ((c) e-posta doğrulaması **gerçek kapanış ama taşıyıcı YOK** — Q02/M7-04) · **🔴 İKİ KANAL KAPATILDI, İKİ KANAL SAYILDI.** Kapatılan: **zamanlama** (dolgu her çıkışı cap'e dolduruyor, maliyeti **ilk adayın kendi digest'inden** — sabitten değil, yani üretimde cost 12 / testte MinCost **inşa gereği**; öncesi **223,7 vs 439,9 ms örtüşmesiz**, sonrası oran **1,000/0,996/1,005/1,011/1,001**) ve **seçici sayısı** (`PickerCap = MaxCandidates−1`; yerleşiğin yuvasını soğuruyor, `pickerCap=7 → sızdıran k: []`, **YÜKÜMLÜLÜK 5 korunuyor**, küme yalnız **daralıyor**) · Sayılan: **kayıt-öncesi kalabalıklaştırma** ve **`signInBlocked` biti** (8 tamamlanmış kayıt/adres, her biri **küresel tekil VAT** ⇒ ~3 saat), ikisi de **gözlenebilir** (`signup.sign_in_unreachable`, kaydın **yarattığı** tenant'a, **adres taşımadan**) · **🔴 VE BİR GÜVENLİK KARARININ DAYANAĞI ÇÜRÜDÜ — ORKESTRATÖRÜNKİ:** dolgu ticareti *"bütçe zaten sekiz karşılaştırma için boyutlanmıştı"* cümlesiyle satın alınmıştı; güvenlik merceği ölçtü — `adminAttemptLimit` **yalnız BAŞARISIZLIKLARI** sayıyor, başarılı giriş yalnız `3000/10dk`'ya tabi, **ve M7-02 geçerli kimlik bilgisini KENDİN-SERVİS yaptı** ⇒ **18/18 başarılı giriş, sıfır 429**, ~15 çekirdek. Düzeltme `adminLoginWorkLimit = 120/10dk/adres` (**ofis modelinden** türetildi, CPU tavanından değil) ⇒ **0,61 çekirdek** — dolgu **öncesi** 1,9 çekirdeğin de altında, yani **M7-02'den ESKİ bir deliği de kapattı** · **§4.5:** enjeksiyon **canlı denendi ve başarısız** (üç adıma `id`/`tenant_id`/`vat_verified=true`/`plan=enterprise` → yeni tenant taze rastgele id, `founding`, `1.50`, hedef tenant **değişmedi**); `tenantID` **`crypto/rand`**, `Draft`'ta id alanı **yok**; `CreateTenant` **üründe yüklem taşıyamayan tek sorgu** ve muafiyeti **kanıtlanıyor** (çağıranın `@id`'si + `WITH CHECK` + `crypto/rand`) · **00017 `admin_users` UPDATE VE INSERT'ünü sütun listesine daralttı** (`arw` → **`r`**; `created_at`/`id`/`tenant_id` **hariç**) — 00017'nin bütün argümanı o sütuna dayanıyordu ve **tablo-genişliğinde yazılabilirdi**; orkestratör geriye tarihli INSERT sondasını **kendi tekrarladı** → `permission denied` · **§4.7:** imzalı state **decode edildi** (parola/e-posta/tenant-id **yok**), 167 satırlık canlı log'da sıfır sır · **🔴 MUTASYON DİSİPLİNİ ÜÇ KEZ YAPICININ KENDİ TESTİNİ YAKALADI** (üçü de hata varken **yeşil**): `PadsEveryExitToTheCap`'in bayat çapası · F2'nin **tüm** hata satırlarını sayması · ve H2'nin **rastgele tenant id'sine** kapsamlanıp INSERT'in **yabancı anahtar** tarafından reddedilmesi — ***"bir reddediş, neyin reddettiğini bilmediğin sürece hiçbir şey kanıtlamaz"*** · **3152 PASS / 0 FAIL / 0 SKIP · 19 paket** |
| M7-03 | Tenant provisioning | **done** — **A** `81ce4d5` (migration **00018**, ADR **0014**, 5 tur / 2 RED) · **B** `8a985eb` (5 tur / **2 RED**). Kartın dört kriterinden **üçü M7-02'de** sevk edilmişti; A M7-02'nin **(b) limitini** kapattı (`password_hash` artık şema düzeyinde işlenebilir bcrypt), B **ilk inilen ekranı** ve **departman cümlesini**. **Beş denetim** (üç genel göz + iki güvenlik geçişi) | `81ce4d5` · `8a985eb` |
| M7-04 | **Şifre sıfırlama ve e-posta taşıyıcısı** *(kart 2026-08-14'te daraltıldı: "Admin daveti" ölçülüp **M7-07**'ye ayrıldı — aşağı)* | **done** — **A** `b039ef3` (migration **00019**, ADR **0015**, 3 tur / 1 RED) · **B** `01a07bc` (**6 tur / 2 RED**, üç **farklı** üçüncü göz + `tappa-security-auditor` **iki geçiş**, ikisi de son turda **ONAY**). Dört kimliksiz rota, **migration YOK** — alıcı adresi mevcut `GetAdminByID`'den, yani *"link yalnız satırdaki adrese"* **yapısal**. **40 mutasyon, 37 yakalandı.** ⚠️ **Beş kriterin ÜÇÜ + iki savunma sevk edilen yapılandırmada ULAŞILAMAZ** (`TAPPA_RESET_DELIVERY=none`, Q02) — **defterde sayılı, kanıtları gözlem değil sahteler** | `b039ef3` · `01a07bc` |
| M7-05 | Hesap ve marka mesajı ayarları | **done** — `145b344`, **6 TUR / 3 RED**, üç **farklı** üçüncü göz + `tappa-security-auditor` **ONAY**. **MIGRATION YOK** (00016'nın beş sütunluk grant'ının üçü yazılıyor). `/admin/account`: künye · VAT + sicil verdiği · kayıt tarihi · zaman dilimi · **salt-okunur** marka cümlesi önizlemesi. **Okuma owner+manager, kayıt owner-only.** 🔴 **Dört kriterin biri M6-12 B'de zaten sevk edilmişti** (fatura/plan görünümü), **üçü ölçülerek düşürüldü** (`vat_number` global UNIQUE → düzenlemesi tenant'ın **dışına** ulaşır · VAT yeniden kontrolü → numara salt-okunurken sicile aynı soru · `locations.timezone` → `checkin.go` **tap edilen** lokasyon için `emp.TenantTimezone` veriyor, o satırlar değişmeden eklenen sütun **yalan söylerdi**). **32 mutasyon, 30 yakalandı** | `145b344` |
| M7-07 | **Admin daveti — bir işletmenin ikinci yöneticisi** *(M7-04 B kapanışında ölçülüp ayrıldı, 2026-08-14)* | **blocked** — **Q02** | 🔴 **Bugün bir işletmenin TAM OLARAK BİR yöneticisi olabiliyor.** `CreateAdminUser`'ın tek üretim çağrı yeri `signup.go:719` ve `role: "owner"` yazıyor (grep) — yani `admin_users.role` sözlüğünün **`manager` değeri üründe ulaşılamaz**, ve onunla birlikte policy motorunun `actor:role` ayrımı, M6-09'un rol kapısı, M6-12'nin owner-only faturası **gerçek müşteride atıl**. Bloke: **Q02** (davet, davetlinin **kendi** adresine gitmek zorunda; taşıyıcı yok → bugün yapılırsa **ikinci bir ölü özellik** sevk edilir) |
| M7-06 | **Operatör içerik ekranı — yasal metinler panelden düzenlenir** *(kullanıcı kararı 2026-08-14: "benden beklenenleri panelden girilebilir yap")* | **done** — migration **00020**, ADR **0016**, 2 tur, üçüncü göz **ONAY** + yapıcının çağırdığı `tappa-security-auditor` **bir sömürü buldu ve kapattırdı**. `.env`'deki `TAPPA_OPERATOR_ADMIN_IDS` + var olan admin girişi + **tek** ekran. **Üç oturumluk kullanıcı beklemesi bitti** | `c8e763a` |

### M8 — [Deploy & pilot](m8-deploy-pilot.md)

| ID | Görev | Durum | Commit / not |
|---|---|---|---|
| M8-01 | Paketleme | **done** — `4ddd11f`, **3 TUR / 0 RED**, üçüncü göz **ONAY** + `tappa-security-auditor` **ONAY**. 🔴 **KARTIN BEŞ KRİTERİNDEN ÜÇÜ ZATEN DOĞRUYDU VE HİÇBİRİNİ HİÇBİR ŞEY TUTMUYORDU** — commit'i Go'nun `-buildvcs`'i gömüyordu ve **kimse okumuyordu**; disk bağımsızlığı bir `http.Dir` uzaktaydı; **`time/tzdata` başka bir paketin (`internal/domain/tap`) tek satırı** yüzünden binary'deydi. Görevin işi **kod yazmak değil, üçünü mekanik olarak denetlenebilir kılmaktı**. Yeni: **`/readyz`** (GET+HEAD, kimliksiz, DB'ye bağlı) · `/healthz`'e HEAD + `no-store` + `nosniff` · `internal/buildinfo` · dar `db.Ping`. **22 mutasyon: 19 test, 1 DERLEYİCİ, 1 kasıtlı yeşil** | `4ddd11f` |
| M8-02 | Barındırma | **FAZ F DONE — `a32d0ca`, 17 TUR / 9 RED**, dokuz farkli ucuncu goz + dort `tappa-security-auditor` gecisi. Kartin *"KEK donme proseduru yazili **ve yurutulebilir**"* kriteri karsilandi: `cmd/rotatekek` (filtre; baglanti/DSN/surucu yok), **iki-KEK okuma yolu** (`sun.UnwrapAny` + istege bagli `TAPPA_TAG_KEK_PREVIOUS`), ve `scripts/rotate-kek.sh`. 🔴 **CEKIRDEK ILK IKI TURDA OTURDU; KALAN ON BES TUR RUNBOOK'A GITTI** — cunku bir runbook **belirtilmemis bir dilde yazilmis bir programdir** (operatorun kabugu, isletim sistemi, `~/.psqlrc`'si, `$PATH`'i hepsi girdi). Bulgular tur tur disari gocdu: testlere → manifeste → kabuga → dosya sistemine → Postgres yapilandirmasina → script'in argv'sine → artefaktlar arasi sozlesmeye. **Ve kusurlar gercekti:** `--help` **butun parki geri alinamaz bicimde donduruyordu** (75.662 satirda olculdu) · `-X` eksikti, dusman bir `~/.psqlrc` `ON_ERROR_STOP`'u **eziyordu** (rc 3→0) ve script *"applied"* deyip 0 donuyordu **hicbir sey yazilmamisken**, ustelik **on kosul 6 saglanmisken sunucu log'una 40 sarmali ref** dusuyordu · runbook *"uygular"* diyordu ama komut bir **dry run**'di (77.215/77.215 satir eski KEK altinda kaldi) · `\!` iceren bir `psqlrc` **iki KEK'i de disari yaziyordu** · ve `GOFLAGS=-toolexec` `go build` sirasinda **gercek yeni KEK'i yakaliyordu** (hicbirimiz aramamistik; `export`→atama-oneki karari onu kendiliginden kapatti). ✅ **URUNUN CALISMA ZAMANI DAVRANISINDA SON UC DENETIM TURUNDA SIFIR BULGU** — dokuz ayri **bagimsiz** AES-256-GCM yeniden-uygulamasiyla dogrulandi: 77.215/77.215 yeni KEK altinda aciliyor ve **duz plaket anahtarlari ozdes**, eski KEK'le **0**, **nonce tekrari yok**, ikinci kosu **bayt-ayni idempotent**. **§4.4:** rotasyon uygulanirken **60 escaman sayac ilerletmesi → 60/60, tam +60, kayip guncelleme yok**; rotasyon `last_ctr`'a **hic dokunmuyor**. **§4.3:** uretilen SQL'de `transactions`/`audit_log`/`last_ctr` **sifir gecis**. **§4.5:** uretilen SQL **saymadan once oturumu bagliyor** (`app.tenant_id` unset **ve** `rolsuper OR rolbypassrls`) — bu olmadan daraltilmis bir oturum **73.100 satirin 15'ini** dondurup **her yuzey basari bildiriyordu**. **§4.7:** sarmali ref'ler **COPY verisi** olarak gidiyor, ifade metni degil (eski sekil **40 ref**, yeni sekil **0**). ⚠️ **SAYILMIS LIMITLER (runbook + kart):** prosedur **kumeye karsi hic kosulmadi** · rotasyon **var olan yedekleri dondurmuyor** · arac **hicbir imajda yok** · managed Postgres **BYPASSRLS on kosulunda cikmaza giriyor** · rotasyonun **`audit_log` izi yok** · `lock_timeout='5s'` **rotasyonun kendi kilit beklemesini** sinirliyor, **tap'inkini degil** (17. turda olculdu: tap 10,07 sn bekledi) · yapistirma dedektoru **calisan bes yapinin ikisini** goruyor. 🔴 **ORKESTRATOR HATASI, kayit icin:** *"kapatma, say"* kurali (agent-brief) **9. turda uygulanmaliydi, 18'e kadar uygulanmadi** — her *"duzelt+pinle+belgele"* yeni yuzey acti (`packaging_test.go` 155→793 satir). Ayrica butunluk hash'i **on bir tur boyunca untracked dosyalari kapsamadi** (yani isin cekirdegi muhrun disindaydi) ve bir brief **denetci kosarken** gonderildi.  **M8-02 DONE (FAZ A–F).** ⚠️ **Karsilanmayan uc kriter SAYILMIS LIMIT olarak kapali, sessizce degil:** *"managed Postgres"* (kullanici karariyla duz Postgres, tek node `local-path`) · **DPA/Q23 + saklama suresi** (hukuki, backlog **B3**) · *"deploy penceresi vardiyalara gore"* (`maxSurge:1/maxUnavailable:0` sevk edildi, **soguk node cekme suresi olculmedi ve olculmedigi yazili**). ✅ **2026-08-17: DEPLOY ILK KEZ UCTAN UCA YESIL** (`32032319245`, 4m2s) — `01-rbac.yaml` kumeye **uygulandi** ve `db-role` kapisi **dogdugundan beri ilk kez gercekten kostu**; ayni daraltma `secrets`'i **tum secret'lar + 7 fiil**'den **`tappa-secrets` + `get`**'e indirdi → **`TAPPA_TAG_KEK` deploy kimliginin patlama yaricapindan CIKTI** (`pods/exec` yalniz `tappa-postgres-0`, uygulama pod'unda **hayir**). Docker Hub kuruldu (iki secret + iki **Public** depo, PAT `pull,push`). 🔴 **Kumede hala yedek YOK → T45**, ama **bugun acil degil**: uretim bos (olculdu).  **FAZ D:** `1b2d623`, **9 TUR / 5 RED**, iki denetçi ONAY. T44 (deploy'un kimliği ilk kez repoda ve **ölçerek daraltıldı**) + T43 (registry → **public Docker Hub**, kusurun kökü kaldırıldı). 🔴 **İki yetki düşürülemez çıktı, ikisi de FAIL-OPEN üretiyordu:** `apps: watch` yokken `rollout status` **exit 0 veriyor ama hiçbir şey gözlemlemiyor**; `namespaces: patch` **naif testte** görünmüyor. Ve **`pods/exec` hiç yoktu** → *"uygulama asla migration rolüyle bağlanmıyor"* kriterinin tek çalışma-anı kanıtı **doğduğu günden beri hiçbir şey ölçmemiş**. **FAZ E:** yedek + provalı geri yükleme; **geri yükleme fiilen yapıldı** (2 118 896 satır / 546 MiB, üç kontrollü koşu). 🔴 **VE PROVA KİMSENİN ÖNGÖRMEDİĞİ BİR KUSUR BULDU:** taze bir pod'a düz `psql < dump`, `tappa_app`'e **31 fazla yetki** veriyor — `transactions` UPDATE/DELETE **ve `tags` UPDATE/DELETE** dahil; ikincisi **§4.4'ün replay korumasını tamamen düşürüyor** (DELETE+INSERT ile `last_ctr` sıfırlanıyor, tetikleyici INSERT'te ateşlemiyor) ve `aes_key_ref`'i **yazılabilir** kılıyor. Sebep: `01-roles.sql`'in `ALTER DEFAULT PRIVILEGES`'ı geri yüklemede her `CREATE TABLE`'da ateşliyor, pg_dump telafi edici REVOKE'ları yaymıyor. **KALICI ÇARE UYGULANDI VE ÖLÇÜLDÜ:** varsayılan `GRANT SELECT, INSERT`'e daraltıldı → geri yükleme artığı **76 → 50**, ve replay zinciri artık **ilk adımda ölüyor** (`ERROR: permission denied for table tags`), `aes_key_ref` UPDATE **false**. Taze kurulumu **bozmuyor**: gerçek goose imajıyla iki kurulum, **20/20 migration**, uygulama tablolarının yetkileri **birebir**, tek fark goose'un kendi defteri; `go test -race` **22 paket, 0 FAIL, 0 SKIP**. ⚠️ **Ama sıfıra inmiyor (5 kalıyor, hepsi SELECT/INSERT) ve dump'ın son satırı varsayılanı HER geri yüklemede yeniden GENİŞLETİYOR** — yani *"unutulan GRANT fail-closed"* vaadi yalnız dar varsayılanın yürürlükte olduğu veritabanlarında geçerli, ve **dev/CI üretimden KATI**, yani ayrım kusuru gizler yönde.  **Bunu güvenlik denetçisi buldu — genel göz aynı diff'e ONAY vermişti.**  FAZ C: `0ce5615`, **11 TUR / 5 RED**, üçüncü göz **ONAY** + `tappa-security-auditor` **ONAY**. T41 **kapandı** (teşhis yanlıştı: etiket değil, k3s'in **asenkron izin kümesi yarışı** — izinli etiketli 5 taze pod, **5/5 ilk denemede ret**; çare kuralda değil **pod'da**), T42 **kapandı** (yazılı gerekçe yanlıştı, bekleme **vardı**; gerçek pencere **DNS'in ~3 sn gecikmesi**), T43 **genişledi ve kullanıcıya geçti** (sebep sıralama değil **KEP-2535**; `rollout undo` **401 alır**). 🔴 **Beş RED'in dördü tek sınıftan: ölçülmeyen yol, gerçekte en olası olan yoldu** — en ağırı, kurtarma adımının `grep -v '^$'` yüzünden **her sağlıklı deploy'u** öldürmesiydi ve onu **güvenlik denetçisi** buldu, genel göz aynı diff'e ONAY vermişken. **FAZ D'ye kalan iki açık kriter: yedek + PROVALI geri yükleme · KEK döndürme aracı.** | Q08 ✅ · Q12 ✅ · **FAZ F** |
| M8-03 | Gözlemlenebilirlik | **done** | `624c555` + `ae07e7c` · **6 yapıcı turu / 3 RED** · genel üçüncü göz **3 tur** (1. RED: 2 davranış + 3 metin · 2. RED: 0+4 · son: kod ONAY, belge RED) + `tappa-security-auditor` **2 geçiş** (1. RED: 3 §4.7 zayıflığı · 2. **ONAY**) · 🔴 **karar motoru bugüne kadar TEK BİR VERDICT SATIRI BİLE loglamıyordu** — kartın beş sinyalinin **beşinin de olayı yoktu**, ve 5xx için hiç erişim kaydı yoktu · 6 olay sevk edildi (`tap.decision` `ctr_gap` **her kararda** · `tap.security_alert` · `readiness.lost`/`.regained` · `http.request`), adları **iki yönde** pinli (sabit **ve** runbook'un yapıştırılabilir sorgusu) · 🔴 **log'u zaten bir MAKİNE okuyormuş ve kart bunu bilmiyordu** (SigNoz ajanı, `exclude` `tappa`'yı saymıyor, logfmt ayrıştırıcısı yok) → prod `json`, geçersiz yazım **boot'u reddediyor** · korelasyon **32/349** çağrı yeri (T51'in ölçtüğü paket çapı dönüşüm **yapılmadı**, daraltma sayıyla yazıldı) · sonda dışlaması **yola göre değil TASARLANMIŞ DURUMA göre** (`/readyz` **500** hâlâ ERROR — toplu dışlama onu ürünün tek sessiz 5xx'i yapardı) · **§4.7'de iki boşluk kapandı:** tam GPS'in **hiçbir mekanizması yoktu** (şimdi 5 metot + R7c), ve **R7 çok satırlı argümanları hiç görmüyordu** — kartın kendi iki olayı tam o kör noktadaydı · 🔴 **VE KART KENDİ SALDIRI YÜZEYİNİ AÇMIŞTI:** `X-Request-Id` istemci kontrollü, ölçüldü **900 KB → 921.757 baytlık tek kayıt**, 30 istek node'un **50 MiB'lik tüm penceresini** siliyordu (ve o pencerede yalnız log'da yaşayan güvenlik uyarıları var) → sınırlandı, **183 bayt** · **1779 PASS / 0 FAIL / 0 SKIP · 23 paket** · `internal/sun` %97,0 · ratchet 53/53 · migration YOK · yeni bağımlılık YOK · ⚠️ **teslimat kanalı YOK ve dürüstçe yazıldı → Q28** · ⚠️ saklama bir **süre değil boyut** (10Mi×5), gerçek üst sınır *"bir sonraki deploy"*, SigNoz TTL **doğrulanamadı** |
| M8-04 | Güvenlik denetimi | **done** | FAZ A (tam repo denetimi + 53 satır triyaj) · FAZ B1 `6f4df34` · FAZ B2 `239d427` · **FAZ B3 `73dd3b9`** (+ `ac29cf7`, aşağıdaki orkestratör hatası). **Toplam 6+6 yapıcı turu / 15 bağımsız denetim, 12'si RED.** **Kabul kriterleri:** 1 ✅ (denetim koştu; ⚠️ *R1–R8 raporu ağaçta bir dosya olarak yok, `state.md`'de anlatılıyor*) · 2 ✅ **`make audit` exit 0** — **T31 KAPANDI**, `go1.26.7`, `govulncheck exit=0` + `redline exit=0` (orkestratör kendi ölçtü) · 3 ✅ (17 backlog satırı ölçüldü; ADR 0005'te **13 satırlık kabul tablosu**, 3'ü *"mekanik çapası yok, sayıldı"* damgalı, sayılar `TestADR0005_TheAnchorCountsMatchTheProse` ile **bağlı**; ⚠️ **FAZ A'nın F7 metni hâlâ repoda yok** — orkestratör borcu) · 4 ⚠️ **kısmen**: çapraz-tenant · oturum çalma · oran sınırı ölçüldü ve karta yazıldı, **gerçek etiketle replay M8-05 FAZ B'ye BLOKE** (NTAG yazıcı donanımı yok) · 5 ✅ ve bu turda **bir kez daha ölçülerek** doğrulandı (`redline-check exit 0` iken tam bir yazıcı boru hattı altından geçti). 🔴 **FAZ B3'ÜN UYGULAMASI DENETLENMEDEN COMMIT EDİLDİ VE PUSH EDİLDİ — ORKESTRATÖR HATASI:** B3'ün ilk yapıcısı koştu ve işini yazdı, ama süreç **SIGTERM** aldığı için harness *"kullanıcı reddetti"* dedi; orkestratör bunu gerçek ret sanıp `state.md` için `git add -A` çalıştırdı ve **22 dosyalık B3 uygulamasını `ac29cf7`'ye** dahil edip push etti (commit mesajı *"open FAZ B3"* diyor, içeriği B3'ün kendisi). Sonradan denetime verildi ve **beş denetim turu, dördü RED** çıkardı. 🔴 **VE BİR RED ÖNCEKİ TURU TERSİNE ÇEVİRDİ:** *"tenant sahte kanıt cümlesi yazdırabiliyor"* iddiası **hiçbir ürün yolundan üretilemiyor** (`copyOfShipped` ifadeyi **bütün** kopyalıyor — koşul ve reason birlikte —, `AuthorCommand`'da `Reason` alanı yok, panel formunda alan yok); önceki tur **bugün doğru olan cümleyi** üç dosyadan silip **yanlışını bir kapıyla dayatmıştı**. Kapı silindi, yerine **yazma yollarını** koruyan türetme geldi: yazıcı kümesi `db/`'den her `-- name:` sorgusunun **ne yaptığına** bakarak çıkarılıyor, `internal/store` **taranıyor**, ham SQL `go/parser` ile okunuyor, ve SQL **gerçek bir lexer**'la yorumlardan ayıklanıyor (`sqlc v1.28` yorumu üretilen sabite birebir taşıdığı için `INSERT INTO /* x */ policy_versions` **`make gen` yolundan** geçiyordu). ⚠️ **Son düzeltme (D54/D55) bağımsız bir denetim turu GÖRMEDİ** — orkestratör kendi ölçtü (redline·audit·build·vet **0**, iki tam suite **0 FAIL / 24 paket**). **Sayılmış, iddia edilmiş değil.** **Yeni backlog: T57** (`TestAppendOnly_TruncateCascadeIsRefusedToo` paralel koşuda **deadlock** — CI'ı ısıracak) · **T58** (tap onay ekranı katman işareti basmıyor — §9, ürün kararı) · **T59** (çalınmış tap oturumu **iptal edilemiyor**; `Revoke`/`ListForEmployee` **üretimde sıfır çağıran**). **FAZ B2: 6 yapıcı turu / 10 bağımsız denetim, 8'i RED** — genel üçüncü göz 5 tur (4. turda ONAY), `tappa-security-auditor` 5 geçiş (son **ONAY**) · 2869 ekleme / 29 dosya + 6 yeni yol · migration YOK · yeni bağımlılık YOK · 🔴 **ürün, RLS'i tamamen öldüren bir DSN ile itirazsız açılıyordu** (`DATABASE_MIGRATE_URL` unset + owner DSN → `healthz=200`, ölçüldü: `tappa_app` **1 satır**, `tappa_owner` **311.129**) → `db.New` **dört olgu** okuyor (`rolsuper`·`rolbypassrls`·RLS'li tablo **sahibi**·o rolün **üyesi**) ve prod'da **reddediyor**; kapı `main`'de değil `db.New`'de, çünkü **unutulabilen bir kapı yapı değil disiplindir** (ADR 0002) · 🔴 **İKİ KIRMIZI ÇİZGİNİN HİÇBİR MEKANİK KORUMASI YOKTU:** `NO FORCE ROW LEVEL SECURITY` sahibe **404.335 satır / 80.961 tenant** açıyordu ve **`internal/db` suiti tamamen yeşil kalıyordu** (izolasyon suiti `tappa_app` ile koşuyor, `FORCE` ise tam olarak **sahibi** bağlayan şey) — 17 tenant tablosunun **11'inde** hiç iddia yoktu, artık **canlı katalogdan türetilerek** hepsinde var · ve `tappa_owner`'ın sahibi olduğu bir **view** RLS'i definer olarak değerlendiriyor: aynı bağlantıda tabloda **2**, view'den **392.105** satır · 🔴 **§5:** kimseyi ayırt edemeyen saklı bir aralık yer kanıtı değildir — **aynı yüklem** artık yazma ve okuma tarafında (`internal/netx`), çünkü `transactions` **değişmez** ve yazma kapısı geçmişi düzeltemez: **dört satır zaten böyle bir aralıktan `ip_match=TRUE` taşıyor**. Ölçü **büyüklük**, kapsama değil — *"dışarıda bir şey bırakıyor mu"* **üç turda üç kez** aşıldı, çünkü uzayı **isim sayarak** tanımlıyordu · ayrıca T53 (sabit-zaman envanteri, **14 çağrı/9 dosya** — backlog'un "18/10"u bir **grep** sayısıydı) · T20 (11 kapsamlı INSERT) · T23 (yanlış müşteri cümlesi; **48.763 tenant** eski metni taşıyor, `BaselineVersion` **bilerek** yükseltilmedi) · T37 (zone düzenlemesi ilk faturalanabilir ayı **kaydırıyordu**) · T27/T3 **kabul, ölçülerek** · `make test` artık dört kırmızı-çizgi testini **sessizce atlamıyor** |
| M8-05 A | Plaket encode runbook — **donanımsız yarı** | **done** | `<M8-05A>` · **üçüncü göz 3 tur (2 RED) + `tappa-security-auditor` ONAY** · **Q10 karara bağlandı: kendimiz encode ederiz** (tedarikçi encode ederse anahtarları tedarikçi bilir = Q06'nın reddettiği tek-nokta riski, bir kat yukarıda) · encode ayarları tablosu ADR 0003'ten **normatif**, her satır AN12196 **revizyon-kapsamlı** atıflı · anahtar teslimi/döndürme (plaket anahtarı **dönmez**, normatif yol `retire + replace`) · anahtar hijyeni **6 mekanizma olarak** doğrulandı · plaket baskısı + QR'ın §5 bedeli · **Q08 uyarısı** (yanlış host'la encode = sahada plaket değişimi) · 🔴 **bölüm "BU BİR PROSEDÜR DEĞİLDİR — BUGÜN ENCODE ARACI YOK" diye açıyor** · araç yolu **karar önerisi** olarak yazıldı, seçim kullanıcının (§1) — ⚠️ **maliyet 2026-08-20'de ÖLÇÜLDÜ** (dört yol, `deploy/README.md`), o günkü *"ölçülmedi"* cümlesi artık bayat · **sayılmış limit:** bu belgedeki hiçbir atıf mekanik olarak doğrulanmıyor (8 uydurma atıf denendi, 8'i de geçti) |
| M8-05 B1 | Encode aracı — **karar ve şartname** (ADR, kod yok) | **done** | `a5b3c2a` · **9 yapıcı turu / 8 bağımsız denetim, 7'si RED + 8'incisi ONAY, 24 bloklayan** (5 genel üçüncü göz — hepsi ayrı ajan — artı `tappa-security-auditor`) · **ADR 0017** yazıldı (396 → ~700 satır, beş düzenleme turu) · kullanıcı **kendi Android app'imizi** seçti, mimari **APDU rölesi** (telefon bayt taşır, kripto sunucuda, düz anahtar **çıkmaz**) → **ADR 0003 md.5 TADİL EDİLMEDİ** · **`K_SDMFileRead` = `0x01`** ve **anahtar `0x00` kişiselleştirilir, EN SON** · encode satırını **`tappa_app`** yazar · **ADR 0005 altıdan SEKİZ riske** çıktı ve sayı artık `TestADR0005_TheRiskCountMatchesTheTable` ile **bağlı** (bu turun tek kod ürünü; kapı **iki turda oturdu** — ilk hâli 2+3 mutasyonla, yeniden yazılan hâli **4+6** ile sınandı; ayrıntı oturum günlüğünde) · **pilot kapısı altıdan YEDİ maddeye** çıktı. 🔴 **BULGULARIN ORTAK SINIFI, yirmi dördünde de aynı: ÖLÇÜMLER SAĞLAM, CÜMLELER ÖLÇÜMLERDEN GENİŞ.** En ağır dördü: §5.1'in normatif sırası **kendi *"anahtar 0 en sona"* kuralını ihlal edebiliyordu** (numara 0 olabilir — DS T11/T14/T69) · §3'ün **§4.7 uyum argümanı kendi §5.1/§2.2'si tarafından çürütülüyordu** (`Zero` *"Wrap'ın hemen ardından"* yazılıydı ama dizide **altı APDU turu** sonra) · **çapraz-tenant UID işgali** (`tags.uid` global PK: RLS satırı gizliyor, PK **varlığını ele veriyor**; işgal edilen uid görülemez/yazılamaz/**silinemez**) · ve M8-06'nın kabul kriteri **kendi kapısının altısını sayıyordu**, yani vektör canlıyken *"kapı tam"* denebilirdi. ⚠️ **Ve *"kapatma, say"* uygulandı:** AN12196 revizyon eşleme sicili **dört turda dört kez** yanlışlandı → **evrensel tamlık iddiası DÜŞÜRÜLDÜ**, tablo ne olduğuyla adlandırıldı, mekanik kapı **T62**'ye. Yeni backlog: **T60** (`redline-check.sh` `docs/`+`.claude/`'ya hiç bakmıyor — mekanik ağ bu turun **asıl ürününün** üzerinden geçmedi; patlama yarıçapı ölçüldü: 366, **298'i R7 desen uyumsuzluğu**) · **T61** (gömülü-anahtar kuralı `git ls-files` → **untracked bir sır görünmüyor**) · **T62**. **Vektör keşfi:** AN12196 **rev. 1.8** §6.6 T14 · §6.9 T19 · §6.16 T26/27 (**rev. 2.0:** §5.6 T14 · §5.9 T18 · §5.16 T25/26 — revizyonsuz bir *"§6"* rev. 2.0'da **"Special functionalities"**a düşer) **üçü de tam çalışılmış örnek** ve erişilebilir (**403 yok**; veri sayfası da HTTP 200 — runbook'un *"erişilemedi"* cümlesi **çürütüldü**). ⚠️ **İki eksen KAT ile kapanmıyor:** plain SDM `CmdData` düzeni yalnız **NT4H2421Gx §10.7.1 T69**'dan · ve **`ChangeKey`'in XOR yarısını ayırt eden hiçbir yayımlanmış vektör yok** (iki örnekte de eski anahtar sıfır) — **M2-08 sınıfı, FAZ B2'nin en büyük riski** |
| M8-05 B2a | Encode aracı — **EV2 kripto çekirdeği** (saf, KAT çivili) | **done** | `c944431` · **4 yapıcı turu / 3 bağımsız denetim (2 genel üçüncü göz + `tappa-security-auditor`), 1 bloklayan + 11 bloklamayan** · `internal/sun/{ev2,changekey}.go` + 3 test dosyası · **kapsam %95,9** (hedef %90+) · **yeni bağımlılık YOK** · migration YOK. 🔴 **XOR ARTIK BİR ARGÜMAN DEĞİL, TEKRARLANMIŞ BİR DENEY:** `ChangeKey` gövdesinden XOR silinince **tam olarak BİR** test kırmızıya dönüyor — **türetilmiş** sıfırdan-farklı-eski-anahtar vakası —, **29 yayımlanmış-vektör testinin hepsi yeşil kalıyor**, iki uçtan uca C-APDU dahil. ADR 0017 §6 md.6 **üretildi**. · **11 bayt-sırası mutasyonu, 11'i de kırmızı** · 🔴 **Belgenin İKİ İÇ ÇELİŞKİSİ ölçümle çözüldü** (aday okumalar yayımlanmış CMAC'e karşı hesaplandı): T17'nin `CmdHeader`'ında **adım 4/12 bayat bir uzunluk basıyor, C-APDU kazanıyor** · T28'in yanıt MAC'inde **adım etiketi `Cmd` listeliyor, veri sayfası listelemiyor ve veri sayfası yayımlanmış değeri üretiyor**. İkisi de **koşan test**, yorum değil. · **`CRC32NK` yazımı kapandı**: dört diziliş `hash/crc32`'den hesaplanıp yalnız birinin (**tümlenmemiş register, LSB-first**) `789DFADC`'yi ürettiği gösterildi. 🔴 **BLOKLAYAN sabit-zaman envanteriydi** (`cmd/tappa/constanttime_test.go`, **M2-08 için yazılmış iki yönlü ratchet**): yeni kripto iki karşılaştırma ekleyip **kaydolmadan geçti**, yani `make check` kırmızıydı — yapıcı yalnız `internal/sun`'ı ölçmüştü. · **`ChangeKeyData` artık all-zero bir `newKey`'i ve no-op bir yeniden yazmayı REDDEDİYOR** — birincisi ağır, çünkü öyle bir plaket *"encode edildi"* işaretlenir, sarmalı bir **sıfır anahtar** taşır ve **ADR 0017 §5.3 sonda 2'yi BAŞARIYLA geçer**: tespit sinyalsiz risk 8. **All-zero ESKİ anahtar (boş çip, normal vaka) hâlâ kabul ediliyor** ve bir kontrol alt testi onu çiviliyor; denetçi kapıyı `oldKey`'e çevirip **normal vakayı kırdı → dört test kırmızı, ikisi yayımlanmış vektör**. · 🔴 **`Zero` LİSTESİ ARTIK BİR MEKANİZMA:** güvenlik merceği **on bir `defer Zero(...)`'ın hepsini sildi ve suite YEŞİL KALDI** → `go/ast` ile sayan iki yönlü bir ratchet yazıldı (grep **değil**: yoruma konan sahte `defer Zero(` sayıyı şişirmiyor, ölçüldü), ve **hemen gerçek bir eksiği yakaladı** — `ev2RotateLeft1`'in **isimsiz** sonucu RndB'nin döndürülmüş tam kopyasını **silinmeden** bırakıyordu, aynadaki `ev2RotateRight1` ise **yalnızca bir yerele bağlandığı için** kapsanıyordu: **ayrım gerekçelendirilmiş değil, tesadüfiydi**. · ⚠️ **SAYILMIŞ:** F2'nin sınıfı **iki okumayla** kapalı, **mekanizmayla değil** — isimsiz bir anahtar-üreten çağrıyı tespit eden bir test **yok**; yanıt-çözme yolu **tek** dış çapaya (T28) dayanıyor; `RndA` üretimi ve `Zero`'nun **çağrılma** garantisi B2b'nin. · **Ve bir denetim yöntemi kayda değer:** genel üçüncü göz **sıfırdan bir AES-128 + AES-CMAC yazdı**, FIPS-197 + RFC 4493 vektörleriyle doğruladı ve **deponun `cmac()`'ini hiç kullanmadan** her KAT sabitini ölçtü — **M2-04'ün *"bağımsız Python ile kanıtlandı"* iddiasının tam olarak eksik olduğu şey**. · Yeni backlog **T64** |
| M8-05 B2b | Encode aracı — **komut katmanı** | **done** | `c170129` · **4 yapıcı turu / 3 bağımsız denetim** (2 genel üçüncü göz + `tappa-security-auditor`), **3 bloklayan + 14 bloklamayan** · `internal/sun/{apdu,ndef,filesettings}.go` + 2 test dosyası · **kapsam %96,7** · yeni bağımlılık YOK · migration YOK. **Yedi komut:** `ISO SELECT` · `GetVersion` · `AuthEV2First` çerçevesi · `WriteData` · **`ChangeFileSettings`** · `GetFileSettings` · `GetCardUID`. 🔴 **M2-08 RİSKİ BURADAYDI VE VEKTÖRSÜZ ÇÖZÜLDÜ:** yayımlanmış örnek **şifreli PICC** kuruyor, Tappa'nın **plain**'inin vektörü **yok** → **aynı kurucu iki yapılandırmayı da üretiyor** ve yayımlanmışı **yeniden üretiyor**, böylece ortak yapı **çivili**, türetilmiş kalan yalnız **plain alan kümesi**. **Ve iki ofset KOPYALANMADI, ÖLÇÜLDÜ:** test yer tutucu dizilerini **bir tablonun `WriteData` gövdesinde** buluyor ve kurucudan **öteki tabloyu** üretmesini istiyor — **aynı oturumdan iki yayımlanmış dize** —, bu ayrıca ofsetlerin **`NLEN` DAHİL dosya baytı 0'dan** sayıldığını kanıtlıyor. · **40 mutasyon, 39 kırmızı**; **ilk geçişte hayatta kalan ÜÇÜN ÜÇÜ DE gerçek boşluktu** ve üçü de aynı şekil (*yayımlanmış bir değer iki okumayı ayıramıyor*: NDEF boyu **palindrom** · her MAC-ofset çifti **eşit** · sıfır UID **üretici kapısına** takılıp Random ID kapısını konuşturmuyor) → **simetriyi kıran vakalarla** kapatıldı, iddia ederek değil. 🔴 **`FileAR` KARARI (ADR 0017 §6 md.13 KAPANDI): `Read = Eh`, `Write = ReadWrite = Change = 0x01`.** Tablo 9: `Write` **ve** `ReadWrite` ikisi de `WriteData`'ya kapı açıyor, `Change` `ChangeFileSettings`'i yönetiyor → `0h` kalsaydı **halka açık fabrika anahtarı diğer ikisini gevşetirdi**. 🔴 **VE NİTELEYİCİLER DARALMADAN ÖNEMLİ — güvenlik merceği bunu bloklayan olarak buldu:** oltalama **yalnız** elinde *sadece* halka açık anahtar 0 olan tarafa karşı, **yalnız** adım 7 tamamlandıktan sonra, **yalnız** dosya `02h` için kapandı. **`0x01`, ADR 0005 risk 7'nin *"her encode dökümünü görene sızar"* dediği anahtardır** → risk 7'nin sonucu artık *"sahte SUN **VEYA** NDEF repointleme"*, ve eski azaltması (*"§5'in üç kanıtı bağlar"*) **yanlıştı**: repointlenmiş plaketin tap'i **bize hiç ulaşmaz**. 🔴 **Ve ölçüm md.13'ün KAPSAMADIĞI iki yol buldu:** **(a) Yetenek Konteyneri** — Tablo 8 dosya `01h`'e `Write = ReadWrite = 0h` veriyor ve **AN12196 §5.14/§5.15 bunu yalnız izin vermekle kalmıyor, tam C-APDU'sunu YAYIMLIYOR** → dosya `02h`'ye hiç dokunmadan oltalama, kapatan **md.5**; **(b) `Change = Fh` GERİ ALINAMAZ** — veri sayfasında **format/reset komutu yok** (ölçüldü, sıfır eşleşme) → repointlenmiş bir CC **dondurulabilir** (düzeltilemez oltalama plaketi), ya da **boş bir çipte `02h` dondurulup bizim adım 7'miz kalıcı olarak öldürülebilir** (⚠️ **tedarik zinciri**; ve **tespit eden şey bu turda sevk edildi**: §5.3 sonda 1 `Change`'i okuyor). · **Q08 ölçüldü ve pratikte kısıtlanmıyor:** `fileLen = 59 + len(host+path)`, tearing koruması **128 baytta** bitiyor → **69 baytlık host+yol bütçesi**; `tappa.mt/t` → **69**, 35 karakterlik bir host → **96**. Aşan şablon **hata döndürüyor, sessiz kırpma yok**. · ⚠️ **VE BİR SÜREÇ HATASI, yapıcının kendi itirafı:** *"iki turdur PDF'leri **ölçemem** dedim — **hiç denememiştim**; ağ ve `pdftotext` başından beri vardı, ve bedelini **iki denetim** ödedi."* |
| M8-05 B2c-1 | Encode aracı — **oturum deposu ve tur sürücüsü** | **done** | `6bb6cdf` · 🔴 **BU GÖREVİN EN UZUN DENETİM ZİNCİRİ: 12 yapıcı turu / 10 bağımsız denetim (8 üçüncü göz + 2 `tappa-security-auditor`), 42 bloklayan.** `internal/encode` — bellek içi EV2 oturum deposu + dokuz adımlık sürücü; HTTP/DB/sqlc/migration **YOK**, portlar **tüketici tarafında** (`Rows`·`Wrapper`·`Clock`), **stdlib**. Kapsam **%92,6** (127 vaka), `internal/sun` **%96,7**, **76 mutasyon / 67 kırmızı / 9 belgeli hayatta kalan**. ✅ **ADR 0017 §6 md.7 KAPANDI** (TTL **90 sn** tabanı adım tablosundan türetilmiş · **iki** süpürücü · sınırlar **1/3/64** · silme garantisi **YERDEN BAĞIMSIZ** ifade + **sayılmış açık**) · ✅ **md.12'nin dördüncü sonucu** — `GetCardUID` kapısı **adım 4b**'ye, yani **ilk geri döndürülemez komuttan ÖNCE**; **ADR §5.1 tadil edildi**. 🔴 **Bulunan üç GERÇEK üretim kusuru:** plaket anahtarının rastgeleliğini **hiçbir şey ölçmüyordu** (sabit **ve** sıralama, iki ayrı turda) · `Close` **meşgul** bir oturumu sahibinin altından siliyor ve `aes_key_ref`'e **16 sıfır bayt** commit ettiriyordu · context **son alışverişte** ölünce **kişiselleştirilmiş** çip için `Done:false` dönüyordu. 🔴 **Ve baskın kusur sınıfı KOD DEĞİL KANITTI:** *kapalı sayım* **dokuz kez** (üçü mekanikleştirildi, biri **terk edildi** — sayılamayanı saymayı bırakmak sınıfı kapattı) · *testin yorumu gövdesinin sürmediği senaryoyu anlatıyor* **altı kez** (biri **tamamen boştu**) · *yapmadan "yapıldı" demek* **üç kez** (mekanizması bulundu: toplu betik sessizce hiçbir şey yazmamıştı) · ve 🔴 **bir düzeltmenin ÇÜRÜTTÜĞÜ KOMŞU CÜMLELER** **beş kez** — **son ikisini orkestratör kendi doğrulamasında buldu**. **Son üç denetim üretim baytlarında kusur BULAMADI**, üçü de nasıl aradığını yazarak. ⚠️ **Araç kazası kaydedildi:** paylaşılan scratchpad'de **sabit adlı** bir betik başka bir ajanınkiyle ezildi ve **üretim kodunu iki kez sessizce geri aldı** → dört denetçi geri alma taraması yaptı. **Devir listesi kartta 16 madde**, başında *"hiçbir çip encode edilmedi"* |
| M8-05 B2c-2a | Encode aracı — **veri katmanı** (migration · sorgular · portların uygulaması) | **done** | `b5f4390` · 🔴 **10 yapıcı turu / 9 bağımsız denetim (7 üçüncü göz + 2 `tappa-security-auditor`), 32 bloklayan.** **Migration `00022`**: `tags.encoded_at` + **iki trigger** (yaz-bir-kez · **INSERT'te kurulamaz** → satır **damgasız doğar**) · projenin **`tags` üzerindeki ilk INSERT sorgusu** · `Rows`/`Wrapper` uygulamaları · **`audit_log`'a iki olay** (`plaque.loaded` adım 3, `plaque.encoded` adım 9; `actor_id` **NULL**, etiket `detail.claimed_by`). **`tenant_id` AÇIK PARAMETRE**, konumsal — *"bir struct literal alanı atlayabilir, bir parametre atlayamaz"*. ✅ **T16'nın SELECT yarısı KAPANDI:** `REVOKE SELECT ON tags` + dokuz sütunluk `GRANT` → **`aes_key_ref` on sütunun tek okunamayanı**. 🔴 **VE BU TURUN ASIL HİKÂYESİ, ŞEKLİ İTİBARIYLA:** §4.7 duvarı **beş METİN/AST tasarımı** denedi ve **beşi de** bir sonraki denetimin bulduğu bir yazıma yenildi — **kasa katlaması** (`AES_KEY_REF`) · **bütün-satır fonksiyonları** (`to_jsonb(tags)`, tek sütunlukta **hiç `Row` tipi üretmiyor**) · **Unicode escape** · **`[][]byte`** (`:many`) · **alias** (`to_jsonb(g)`) · **`sqlc.yaml` override**. **Altıncı deneme SORUYU DEĞİŞTİRDİ:** ayrıcalık *"bir sızıntı nasıl görünür"* diye **hiç sormuyor**. İki denetçi ona **kırk bir doğrudan şekil** attı, **hepsi `permission denied`**. 🔴 **KAPANMAYAN ADIYLA SAYILDI, kapatıldığı iddia edilmedi** (`agent-brief` durma kuralı 2): anahtar **`resolve_tag_by_uid`** üzerinden okunabilir kalıyor ve **kapatılamaz** — tap **tenant bağlamı olmadan** gelir ve SUN'ı doğrulamak için zarfı **açmak zorundadır** (ADR 0002 md. 7); **o yolu tek çağırana sınırlayan yürürlükte bir mekanizma YOK**, ama **mevcut olan var ve alınmadı** (ayrı çözümleme rolü, bedeli **ikinci bir havuz**); envanter kolları **düz yazımı** yakalıyor ve **kanıtlanmış biçimde dört kaçamaklı yazımı yakalamıyor**; **ve o yol tenant sınırını da aşıyor** (definer sahibi **BYPASSRLS**). **Devir listesi md. 19.** ⚠️ **`internal/encode` hâlâ hiçbir paketten import edilmiyor** — üretimde olayları yazan ve `encoded_at` dolduran **yol YOK** (md. 18, B2c-2b'nin ilk işi) |
| M8-05 B2c-2b | Encode aracı — **HTTP rölesi ve yetkilendirme** | **done** | `f632f09` · 🔴 **11 yapıcı turu / 11 bağımsız denetim (9 üçüncü göz + 2 `tappa-security-auditor`), 10'u RED.** Üç rota panelin yazma grubunun **iç grubunda**: `ProtectWriting()` + `encodeGate`. **Tenant ve aktör OTURUMDAN** — denetçiler **on beş taşıyıcı** denedi (query · JSON · beş başlık · multipart · path · form · çerez · chi param · `Host` · XFF · tekrarlanan anahtar · `Referer`), **hiçbiri geçmedi**. ✅ **md.10 · md.12 · md.18 KAPANDI.** Bütçe **turdan türetilmiş** (`20 × RequestsPerRound`), ilk 429 tam **221**'de. 🔴 **GÖVDE KAPISI ÜÇ TASARIM KAYBETTİ, SONRA SAYILDI:** `body any` (**anonim struct** yendi) → **mühürlü arayüz** (**struct GÖMMESİ** yendi — Go metot terfisi + `encoding/json` düzleştirmesi) → **değer parametresinin kaldırılması** (**muaf yazıcının `w` yetkisi** yendi). Üçünde de sızıntı **gerçek router'ın gövdesine/başlığına çıktı** ve **paket yeşil kaldı**. Sınıf B2c-2a'nın §4.7 duvarıyla **aynı**: *"bir sızıntı nasıl görünür"* sorusunun cevap uzayı **sonsuz**. **Dördüncüsü DENENMEDİ** → **md. 15**. 🔴 **VE 2. GÜVENLİK GEÇİŞİ, BEŞ GENEL DENETÇİNİN GÖRMEDİĞİ GERÇEK BİR ÜRÜN KUSURU BULDU:** `plaque.unmarked`, **kodun kendisinin *"OLAĞAN"* dediği** tetikleyicide (telefon son R-APDU'yu atıp kapanır → istek ctx'i ölür) **hiç yazılmıyordu** — ölçüldü: canlı ctx **1 satır**, iptal ctx **0**. Düzeltme **iki katmanlı**: `markEncoded` **de** kopuk context'te koşuyor (olağan kapanma **ONARILIYOR**, hasar olarak dosyalanmıyor), ve `plaque.unmarked` **anlamı değişerek** kaldı. **`DefaultRepairGrace` = 5 sn**, `2 × 5 ≤ httpShutdownGrace` **kapıya bağlı**; ölçüldü (`pool_max_conns=1`): ölü istek handler goroutine'ini **10,002 sn** işgal ediyor. 🔴 **`audit_log.actor_id` ARTIK GERÇEK `admin_users.id`** — md. 8'in kararı değişti, ADR'de **tarihsel damgayla** yazılı; panelde ad basıyor ve **join'in iki tarafı da `@tenant_id`** taşıyor. **Üçüncü olay `plaque.unmarked`** · **`MaxPerTenant = 8`** (tek tenant depoyu tüketemez; **N-tenant artığı 8 kayıt** olarak sayılı) · **kart sayımı mekanikleşti** (`handoverlist_test.go`) ve **yedi saldırı** yedi. ⚠️ **Devir listesi 19 → 28 madde** (5 kapandı, 23 açık) |
| M8-05 B3 | Encode aracı — **Android rölesi ve fiziksel doğrulama** | todo | **donanıma bloke** · `android/` Go modülünün **dışında**, ayrı `make` hedefi · 9 maddelik devir listesi runbook'ta: gerçek çiple uçtan uca · encode tarafının MSB-first yazdığının doğrulanması (M2-08'in kapatamadığı yarı) · replay denemesi · **Q11** (iPhone Safari çerez ömrü) · fabrika varsayılanlarının değiştiği · yazma izninin kilitlendiği · baskı provası |
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
| M9-08 | **Operatör paneli** *(kullanıcı kararı 2026-08-14: "operatör paneli yapılır oradan kontrol edilir")* | todo | **§4.5 — tenant sınırını bilerek aşan ilk yüzey.** M7-06 dar sürümü sevk etti (tek ekran, tenant verisine bakmaz); borçlar M9-08 kartında |

**Özet:** **89 görev** · done **75** · **wip 0** · **blocked 1** (M7-07, Q02) · skipped 1 · todo **12** · **M0+M1+M2+M3+M4+M5+M6 TAMAM 🎉🎉 · M7 6/7 — kalan tek görev Q02'ye bloke, yani M7 KULLANICI DIŞINDA BİTTİ · M8 4/8** *(2026-08-19: **M2-08** açıldı — M8-05 FAZ A'yı ölçerken `internal/sun`'da **sevk edilmiş bir kripto kusuru** bulundu, ürünün doğruluk çekirdeğinde; 87'den 88'e. Ve **M8-05 A/B'ye bölündü** — kartın 6 kriterinden 4'ü donanımsız yapılabiliyordu, 1'i fiziksel çip istiyor; 88'den 89'a)* *(2026-08-14: M7-04 B kapanışında **M7-07** açıldı — kartın başlığı "Admin daveti" diyordu, beş kabul kriterinin hiçbiri istemiyordu, ve ölçüm gösterdi ki **hiç yapılmamış**; 85'ten 86'ya)* *(2026-08-14: kullanıcı kararıyla **M7-06** ve **M9-08** açıldı → 83'ten 85'e)* — **ürünün fonksiyonel boşluğu kapandı: artık herkes kayıt olabiliyor** *(M5-11 M5-09'da bulunan §5 ihlali için kullanıcı kararıyla açıldı → toplam 82'den 83'e)*

---

## Oturum günlüğü

En üste ekle. Kısa tut: ne yapıldı, ne öğrenildi, ne kaldı.

### 2026-08-25 (17. oturum, üçüncü yarı) — 🟢 **PUSH + DEPLOY: CANLI `af8b44c`, migration 22 ÜRETİMDE**

**Kullanıcı *"push yap deploy alınsın"* dedi.** Yedi commit push edildi, ve **CI kırmızı çıktı** — `TestAuditIndex00021_PlaqueHistoryUsesTheIndex`. 🔴 **Ve bu bir flake değildi: main 2026-08-22'den beri kırmızıydı, AYNI testle** (`32570619152` · `32856990423`), yani **üç gündür hiçbir şey deploy edilmiyordu ve bunu kimse fark etmemişti.**

🔴 **KUSUR TESTİNDİ, PLANLAYICININ DEĞİL.** Test, planlayıcının `audit_log_tenant_target_idx`'i **tercih etmesini** bekliyordu. CI'da tablo küçük → **Seq Scan 6.62'ye mal oluyor**, indeks taramasından **ucuz**; planlayıcı **doğru** olanı yapıyordu. Yerelde yeşildi çünkü geliştirme DB'sinde `audit_log` **~279.000 satır**.

🔴 **VE TESTİN KENDİ ÖLÇÜMÜ ARTEFAKTTI — kök neden bulundu:** *"5 satır taze bir veritabanında zaten yeterli"* diyordu. **Geri alınan sondalar bile ölü heap sayfaları bırakıyor**, yani bir süpürme **tabloyu kendi ayağının altında şişiriyor**. `VACUUM FULL` ile gerçek eğri: **geçiş ~250 satır**, fixture'ın **200'ü onun %20 altında**. ⚠️ **Ve yapıcı ikinci turda AYNI hata moduna kendi düştü** (`VACUUM FULL` ölü tuple'ları siler, **commit edilmişleri değil**) — bu, bulguyu **genişletti**: taban **sabit değil**, önceden commit edilmiş satırlarla **yükseliyor**, çünkü başka tenant'ların satırları ANALYZE'ın `target` seçiciliği hakkında öğrendiğini **seyreltiyor**.

🔴 **(A) ELENDİ, VE GEREKÇESİ MALİYET DEĞİL DAYANIKLILIK:** en küçük kazanan fixture `random_page_cost` **1.1 → 40** arasında **200 satırdan 5000'e**, yani **25 kat** kayıyor — ve **1.1 ile 4'ün ikisi de sıradan ayar**. **Eşik testin seçebileceği bir şey değil.** ✅ **Çare yine SORUYU DEĞİŞTİRMEK oldu:** *"planlayıcı bunu **seçecek mi**"* testin kontrol edemediği değişkenlere bağlı; *"planlayıcı bunu **kullanabilir mi**"* tamamen testin elinde. **0 satırdan 280.090'a, `random_page_cost` 0.5'ten 100'e, düz ve çarpık istatistikte hepsinde aynı cevap.**

✅ **VE YAPICI BENİM ÖNERİMİ ÖLÇÜMLE ÇÜRÜTTÜ, HAKLIYDI:** çıplak `enable_seqscan=off` **yetmiyor** — ANALYZE olmadan planlayıcı **`audit_log_tenant_at_idx`**'i seçip `target`'ı **filtreliyor**, yani **00021 ÖNCESİ şekil**. Fixture ve ANALYZE **hâlâ yük taşıyor**; yalnız *"seq scan'i geç"* işleri gitti.

🔴 **NE KAYBEDİLDİ, DÜRÜSTÇE:** test artık planlayıcının indeksi **üretim hacminde tercih edeceğini kanıtlamıyor** — **T17'nin asıl korkusu tam orada yaşıyor** — ve bu **backlog T17'ye yazıldı**. Eski assertion da onu taşımıyordu (200 satırlık bir fixture üzerinde bir **tercih** iddia ediyordu), ama şimdi **hiçbir kapı** korumuyor. Üretim kanıtı **ölçüm olarak** duruyor: **5574/279436 satırda 1838 buffer / 6,2 ms → 7 buffer / 0,16 ms**, ve denetçi bunu **indeksi geri alınan bir işlem içinde DROP ederek** yeniden ölçtü — yani BEFORE **gerçekten indeksin yokluğu**, bir planlayıcı bayrağı değil.

**Deploy sonucu (`32865969803`).** `goose: successfully migrated database to version: 22` — **`tags.encoded_at` + iki trigger artık ÜRETİMDE**. Rollout başarılı, **§4.5 kapısı geçti** (*"every network connection to the database is `tappa_app`"*), canlı revizyon **`af8b44cebc58` = HEAD**, `modified:false`, `go1.26.6`. 🔴 **Yani encode uç noktası ve `internal/sun`'ın wipe disiplini artık CANLI.**

**Ne kaldı.** Ledger'da **bloke olmayan görev yok** (B3 · M8-06 · M8-07 · M9 — hepsi kullanıcıya ya da donanıma bloke). Hijyen: **T60·T61·T62·T63·T65**; **T65** *"M8 pilot öncesi — CI yeşilliği buna bağlı"* etiketli ve şimdi **CI'ın gerçekten yeşil olduğu** görüldüğü için sıradaki en savunulabilir iş odur.

### 2026-08-25 (17. oturum, ikinci yarı) — **BACKLOG T64 DONE** (`f540f8a`) · kripto wipe disiplini · **6 yapıcı turu / 5 bağımsız denetim, 4'ü RED**

**Neden bu iş seçildi.** 🔴 **Ledger'da bloke olmayan görev kalmadı** ve bu ölçüldü: **B3** Android zinciri **bu makinede yok** (Gradle · SDK · `ANDROID_HOME` · kotlinc · adb — hepsi yok) artı donanım artı md.5/Q08 · **M8-06** Q13+T45 · **M8-07** M8-06'ya bağlı · **M9-01…08** MVP dışı ya da Q22. Hijyen backlog'undan **T64 seçildi** çünkü tek **§4.7** maddesiydi ve ürünün **doğruluk çekirdeğindeydi**.

**Ne yapıldı.** `cmac.go` **hiçbir ara tamponu silmiyordu** — `2380baa`'dan (2026-07-26) beri, **altı üretim çağrı yerinde**, ve **ikisi `verify_mac.go`**, yani **canlı tap akışı**. **34 tampon** deferred-wiped (`cmac.go` · `verify_mac.go` · `ev2.go` · `changekey.go`), muafiyetler için **kapalı gerekçe sözlüğü**, ve `go/ast` üstünde **üç dosyada 146 bildirimi** sınıflandıran bir kapı. **Migration yok, yeni bağımlılık yok, KAT vektörleri dokunulmadı.**

🔴 **EN AĞIRI CBC BİRİKTİRİCİSİYDİ:** dönüş anında **CMAC çıktısının kendisi**, ve `ev2SessionKeys`'te o **birebir `KSesAuthENC`/`KSesAuthMAC`** — yani `ev2SessionKeys`'in **özenle sildiği** oturum anahtarının **silinmemiş ikinci kopyası**, bir katman aşağıda. Ve kaçış analizi onu **heap'e** koyuyor, yani tahsis yeniden kullanılana kadar yaşıyordu.

🔴 **VE BİR MUAFİYET GEREKÇESİ TAM TERSİNİ SÖYLÜYORDU.** `mac` ve `ev2.go`'nun `want`'ı **karşılaştırma** değerleridir, tele **çıkmazlar**; dosya ikisini de *"MAC tag'leri zaten tele giden şeydir"* diye savunuyordu. **Denetçi ölçtü:** `verify.go:199-206` MAC doğrulanmadığında **sayacı ilerletmeden** dönüyor → **reddedilen** bir tap'ten sonra kalan `mac`, saldırganın **sahip olmadığı** `(uid, ctr)` çiftinin **doğru** MAC'i, üstelik `key`/`sessionKey`/`full` o anda **sıfırlanmış**. Kural yeniden yazıldı: *"bir karşılaştırma değeri **asla** muaf değildir"*.

🔴 **VE BU OTURUMUN DOKUZUNCU KAPI YENİLGİSİ BURADA OLDU — DÖRT KEZ.** `Zero(x[:0])` (sıfır genişlik) · **ulaşılmayan `defer`** · yürüyüşün `else if`/etiketli deyim/`select`'e **körlüğü** (iki eski kaçış **birleşik halde** geri geldi) · ve **aynı derinlikteki kardeş bloklar** (gölgeleme **derinlikle** izleniyordu). Dördünde de **paket yeşil kaldı**. ✅ **Çare bir ÖLÇÜT oldu ve kapının başlığına yazıldı:** **bir geliştiricinin KAZAYLA yazacağı şekli kapat; KASITLI kaçamak gerektireni SAY.** *Bu, bir **kapı** ile bir **güvenlik sınırı** arasındaki farktır.* İki kardeş `if` sıradan Go'dur → **kapatıldı**; `if len(x) > 1<<40 { Zero(x) }` ve `*[]byte` parametresiyle dilim taşıyan **var olmayan** bir yardımcı → **sayıldı**.

🔴 **VE GÜVENLİK MERCEĞİ, DOĞRULUK MERCEĞİNİN GÖREMEDİĞİNİ YİNE BULDU** — bu kez ürünün **sayımında**: envanter yalnız `aes.NewCipher` çağrı yerlerini sayıyordu, **`cipher.NewGCM` tamamen dışarıdaydı**. Stdlib kaynağından doğrulandı: `GCM struct { cipher aes.Block }` — **değerle gömülü**. Yani **her tap ikinci bir verbatim KEK kopyası** bırakıyor: **~1 KB referanssız heap, içinde parkın TÜM tag anahtarlarını açan 32 baytlık KEK.** **Kapatılamaz** (Go anahtar programını silmenin yolunu sunmuyor) → **sayıldı**, ve `keys.go`'nun *"no KEK-derived key schedule outlives the operation"* cümlesi **ölçümle değiştirildi: referans yaşamıyor, BAYTLAR yaşıyor.**

⚠️ **VE KAT'LAR BU SINIFA KÖR, ÖLÇÜLDÜ:** tele **giden** tag'e wipe eklemek → 29 yayımlanmış vektör **tamamen yeşil**; `cmac`'ten `defer Zero(x[:])` silmek → yine **yeşil**. **Davranış testleri *"wipe var mı, doğru değişkende mi"* sorusunu göremiyor** — kaynak okuyan kapının neden onların yanında durduğunun kanıtı bu.

⚠️ **İKİ SÜREÇ KUSURU, İKİSİ DE KAYDA GEÇTİ.** (1) **Benim mühürleme yöntemim izlenmeyen dosyayı GÖRMÜYORDU** — dört tur boyunca denetçilere `cmac_wipe_test.go`'yu (1787 satır) kapsamayan bir mühür verdim; **iki parçalı** mühre geçildi. (2) Yapıcının mutasyon harness'ları her dosyayı snapshot'lamadı, biri **çalışırken çöktü**, ağaçta **üç kez artık kaldı** — üstelik **izlenmeyen ve kurtarılamaz** bir dosyanın yanında. 🔴 **Ve son denetçi doğru olanı yaptı:** *"'kapılar her seferinde yakaladı' RETROSPEKTİF OLARAK DOĞRULANAMAZ — 'temiz' demiyorum, 'doğrulanamadı' diyorum."*

**Ne kaldı — iddiasız.** **9 dosyada 179 bildirim** bütünlük kapısının dışında (içeriği elle sınıflandırıldı: **anahtar taşıyıp silinmeyen 0**) · **KEK'in tap başına iki silinemeyen kopyası** · `cipher.Block` ×6 · register skalerleri (`carry`, `raw`) · üç **sayılmış** koşul kenarı (kimlik **ad** bazlı · erişilebilirlik yalnız deferred'a · kararlılık **fonksiyon** kapsamlı) · `unsafe` ile bir `Block`/`GCM` wipe'ının güvenli olup olmayacağı **denenmedi**.

### 2026-08-25 (17. oturum) — **M8-05 FAZ B2c-2b DONE** (`f632f09`) · encode uç noktası · **11 yapıcı turu / 11 bağımsız denetim, 10'u RED**

**Ne yapıldı.** `internal/handler/plaqueencode.go` — üç rota, panelin yazma grubunun **iç grubunda** (`ProtectWriting()` + `encodeGate`). `internal/encode` **artık üretimden çağrılıyor** (`cmd/tappa/main.go` + handler; önce **sıfır** import). Bütçe **turdan türetilmiş**, `audit_log`'a **üçüncü olay** (`plaque.unmarked`), `actor_id` **gerçek admin id'si**, `Config.MaxPerTenant`, ve dört yeni mekanik kapı (`handoverlist_test.go` · `shutdownbudget_test.go` · `relayexposure_test.go` · encode yüzeyinin AST kapıları). **Migration YOK, yeni bağımlılık YOK** (`db/` diff'i **boş**).

🔴 **BU GÖREVİN EN PAHALI DERSİ, VE ÜÇÜNCÜ KEZ AYNI SINIF: GÖVDE KAPISI ÜÇ TASARIM KAYBETTİ.** `body any` → beşinci alanlı **anonim struct** tele çıktı, paket **yeşil**. Mühürlü `encodeBody` arayüzü → **struct GÖMMESİ** yendi (Go metot **terfisi** + `encoding/json`'ın gömülü alanları **düzleştirmesi**); *"anonim struct asla uygulayamaz"* cümlesi **dört kopyada** yanlıştı. Değer parametresinin kaldırılması → **muaf yazıcının `w` üzerindeki tam yetkisi** yendi (`w.Header().Set(...)`, tek satır, **beş AST kapısı kör**). Üçünde de sızıntı **gerçek router'dan** ölçüldü. **Sınıf B2c-2a'nın §4.7 duvarıyla birebir aynı:** *"bir sızıntı nasıl görünür"* sorusunun cevap uzayı **sonsuz**, ve **kompozisyonla** genişletilebiliyor. **Dördüncüsü denenmedi** — durma kuralı 2, ve bu oturumda **üç kez** uygulandı (gövde kapısı · kart sayımının düzyazı yarısı · telafi yazmasının sınırı).

🔴 **VE MERCEK KURALI KENDİNİ BİR KEZ DAHA KANITLADI — BU SEFER EN PAHALI ŞEKİLDE.** İki genel denetçi arka arkaya *"ürün kodunda **DAVRANIŞ = 0**, her mutasyonda tuttu"* dedi. Sonra **ikinci güvenlik geçişi** koştu ve **ilk sondada** gerçek bir ürün kusuru buldu: **`plaque.unmarked`, kodun KENDİSİNİN *"bir HTTP rölesi için OLAĞAN"* dediği tetikleyicide hiç yazılmıyordu** — telefon son R-APDU'yu atıp kapanır, istek ctx'i ölür, **hem işaretleme hem de onun kanıtı aynı sebeple düşer**. Ölçüldü: canlı ctx **1 satır**, iptal ctx **0**. Geriye kalan satır, *"çipe hiç dokunulmadı"* satırıyla **bayt bayt aynı** ve kurtarma talimatları **zıt**; müdürün doğru görünen hamlesi zincirin sonunda **plaket anahtarının tek kopyasını sildiriyor**. **Beş genel denetçi bunu göremedi.** *(M8-04'ün dersi: bir mercek yakınsadı diye görev yakınsamış sayılmaz.)*

✅ **VE DÜZELTMESİ, BİR DENETÇİNİN ÖLÇÜMÜ SAYESİNDE, KUSURDAN DAHA İYİ ÇIKTI.** İlk çare telafi yazmasını kopuk context'e almaktı. Bir sonraki denetçi ölçtü: yayımlanan **sınıf kuralı**, telafi ettiği ifadeyi (`MarkTagEncoded`'ın `encoded_at`'i) **de kapsıyordu** — yani tur **hasarın kaydını** sevk ediyordu, **onarımını değil**. Aynı tek satırlık mekanizma işaretlemeye uygulanınca hasar **hiç oluşmuyor**. İkisi de sevk edildi, gerekçeleri **ayrı**: olağan kapanma **onarılıyor**, `plaque.unmarked` ise **DB gerçekten erişilemezken** yazılıyor.

🔴 **AMA ASIL DERS ORADA DEĞİL: BİR SAYIM ON BİR TURDA ON BİR KEZ EKSİK ÇIKTI.** Her turda yapıcı bir liste yazdı, her turda bir sonraki denetim onu eksik buldu — ve **iki kez düzeltmenin KENDİSİ yenisini üretti** (10. tur *"dört sebepten üçü"* dedi, 11. tur ölçtü: **ikisi**). Çare her seferinde aynıydı ve sonunda uygulandı: **liste değil, ÖZELLİK.** *"Telafi rapor ettiği yolu kat eder — aynı havuz, aynı veritabanı, aynı süreç; yalnız o yolu bozulmamış bırakan bir arızayı kaydedebilir."* Altında: **bir kaynağa yapılan yazma, o kaynağın kendi arızasının tanığı olamaz.** Ve doğru sonuç ondan **türetiliyor**, sayılmıyor: **satır varsa DB erişilebilirdi; satır yoksa hiçbir şey kanıtlanmaz.** ⚠️ Bunun tersi (*"satır = DB erişilemezdi"*) **altı yerde** geri çekilmeden duruyordu, ve **ikisi kendi düzeltmesiyle yedi/on üç satır arayla çelişiyordu.**

⚠️ **BİR ORKESTRATÖR KUSURU, VE BRIEF'İN KENDİSİ ÜRETTİ.** Yapıcı, benim `state.md`'de yaptığım dört düzeltmeyi *"yasak dosyaya dokunmuşum"* diye **HEAD'e geri aldı ve sildi**. Yasak liste, ajana **orkestratörün meşru işini kendi ihlali** gibi okuttu. **Kural eklendi ve kalan turlarda uygulandı: bir yasak dosyada değişiklik görürsen RAPOR ET, GERİ ALMA.**

**Ne kaldı.** **B3** (Android rölesi) — **M8'in tek donanıma bloke fazı**, ve encode aracının **son parçası**. **Md. 16 hepsini yönetiyor: hiçbir çip encode edilmedi**; on bir turluk sunucu tarafının **tamamı** belge okuması, sahte çip ve mutasyondur. Devir listesi **28 madde (5 kapandı, 23 açık)** ve sayısı artık `cmd/tappa/handoverlist_test.go` ile **mekanik** — üç turda üç kez elle yanlış yazıldıktan sonra. **Kullanıcıda:** md. 5 (anahtar 0'ın şeması, **pilot kapısının 7. maddesi**) · md. 11 = **Q08** · **md. 17** (ayrı çözümleme rolü + ikinci havuz + yeni Secret) · ve **T45** (yedek — **pilotun gerçek bloklayıcısı**).

### 2026-08-24 (16. oturum) — **M8-05 FAZ B2c-2a DONE** (`b5f4390`) · veri katmanı · **10 yapıcı turu / 9 denetim / 32 bloklayan**

**Ne yapıldı.** Migration **00022** (`tags.encoded_at` + **iki trigger**: yaz-bir-kez, ve **INSERT'te kurulamaz** → satır **damgasız doğar**) · projenin **`tags` üzerindeki ilk INSERT sorgusu** · B2c-1'in üç portunun uygulaması (`DBRows`, `KEKWrapper`) · **`audit_log`'a iki olay** (`plaque.loaded` adım 3, `plaque.encoded` adım 9; `actor_id` **NULL** ve etiket `detail.claimed_by`'da, *"kimse doğrulanmamış bir dizeyi bir ekranın ad diye bastığı sütuna terfi ettirmesin"*) · **`tenant_id` AÇIK PARAMETRE**, konumsal (*"bir struct literal alanı atlayabilir, bir parametre atlayamaz"*). Kapsam **%93,3**, **146 vaka, 0 SKIP**.

✅ **T16'nın SELECT yarısı kapandı.** `REVOKE SELECT ON tags FROM tappa_app` + **dokuz sütunluk `GRANT`** → **`aes_key_ref` on sütunun tek okunamayanı**. Bedeli ölçüldü ve **sıfır değil**: **üç test assertion'ı** kırıldı, üçü de **`tappa_owner`'a taşındı** — *"kırılan tek şey zarfı okuyabileceğini varsayan testlerdi, ki bu değişikliğin işe yaradığının kanıtı."* **INSERT yarısı açık ve KAPATILAMAZ** (§3.1 `tappa_app`'i zorunlu kılıyor).

🔴 **ASIL DERS BİR YÖN DEĞİŞİKLİĞİ, ve beş kaybedilmiş turla ödendi.** §4.7'nin *"`aes_key_ref` sürece sızmasın"* duvarı **beş metin/AST tasarımı** denedi ve **beşi de** bir sonraki denetimin bulduğu bir yazıma yenildi: **kasa katlaması** (`AES_KEY_REF` — PostgreSQL tırnaksız tanımlayıcıyı küçük harfe katlar) · **bütün-satır fonksiyonları** (`to_jsonb(tags)`, ve **tek sütunlukta hiç `Row` tipi üretmiyor**) · **Unicode escape** (`U&"\0061es_key_ref"`) · **`[][]byte`** (`:many` biçimi) · **alias** (`to_jsonb(g)` — ve `tags.sql`'in kendi okumaları **zaten `FROM tags g`** yazıyor) · **`sqlc.yaml` override** (tek satır, `[]byte` → `json.RawMessage`). **Sebep yapısal:** *"bir sızıntı NASIL GÖRÜNÜR"* sorusunun cevap uzayı **sonsuz**. **Ayrıcalık sistemi o soruyu HİÇ SORMUYOR** — sütun okunamıyorsa hangi ifadeyle denendiği **önemsiz**. İki denetçi ona **kırk bir doğrudan şekil** attı (`SELECT *`, `COPY`, `MERGE … RETURNING`, LATERAL, `md5(key::text)`, `ONLY tags`, `CREATE VIEW`…), **hepsi `permission denied`**.

🔴 **VE KAPANMAYAN, KAPATILDIĞI İDDİA EDİLMEDEN SAYILDI** — `agent-brief.md`'nin **ikinci durma kuralı** (*"yeni kanal kapatılmaz, sayılır; sayılmış bir açık, kapatıldığı İDDİA EDİLEN bir açıktan güvenlidir"*). Anahtar **`resolve_tag_by_uid`** üzerinden okunabilir kalıyor — ve **kapatılamaz**, çünkü tap **tenant bağlamı olmadan** gelir ve SUN'ı doğrulamak için zarfı **açmak zorundadır** (ADR 0002 md. 7). Sayımın dört parçası, hepsi ölçülmüş: **yürürlükte** bir mekanizma yok · **ama MEVCUT OLAN VAR ve ALINMADI** (ayrı çözümleme rolü, `REVOKE EXECUTE` + ayrı `GRANT`, bedeli **ikinci bir havuz**) · envanter kolları **düz yazımı** yakalıyor ve **kanıtlanmış biçimde dört kaçamaklı yazımı yakalamıyor** · **ve o yol tenant sınırını da aşıyor** (definer sahibi **BYPASSRLS**). **Devir listesi md. 19.**

**Ne öğrenildi — üç kural, üçü de ödenerek.**
1. 🔴 **Bir kapı, cevap uzayı sonsuz olan bir soruyu cevaplamaya çalışıyorsa kaybeder.** Beş tasarım, beş kaçış kümesi. Çare kapıyı iyileştirmek değil, **soruyu değiştirmek**ti.
2. 🔴 **Yanlış bir hüküm OPERATÖRÜ ARAMAYI BIRAKTIRIR.** *"Mekanik hiçbir şey yok"* yazılmıştı; bir denetçi tek transaction'da **`proacl`** üzerinde çürüttü. Ve **ters yönü de aynı derecede pahalı**: *"suite'i yeşil bırakır"* diye sayılan bir açık **gerçekte yoktu** — komşu bir paket (`query_test.go`) onu **zaten yakalıyordu**, ve yanındaki paragraf *"neden parse etmiyoruz"* diye **zaten koşan bir parser'a karşı** argüman kuruyordu.
3. 🔴 **Bir sayı, KAÇ KOPYADA YAŞIYORSA o kadar yerde düzeltilir.** *"Ten of eleven"* karta yazıldı, **koda ve migration'a yazılmadı** — ve o listedeki iki ret **00022'nin üretmediği** retlerdi (**kendi payı sekiz**). Aynı sınıf bu görevde **defalarca**: *"yapmadan yapıldı demek"*, ve **bir düzeltmenin çürüttüğü komşu cümleler**.

**Ne kaldı.** **B2c-2b** (HTTP rölesi + yetkilendirme, **donanımsız**) ve **B3** (Android rölesi, **tek donanıma bloke faz**). Kartın **"KAPATILMAMIŞ SAYIM"** listesi **19 madde** ve bir sonraki oturumun okuyacağı tek kayıt; **md. 18** (`internal/encode` **hiçbir şeyden import edilmiyor** → üretimde olayları yazan ve `encoded_at` dolduran **yol yok**) **B2c-2b'nin ilk işi**. **Md. 16 hepsini yönetiyor: hiçbir çip encode edilmedi.**

### 2026-08-21 (15. oturum, altıncı yarı) — **M8-05 FAZ B2c-1 DONE** (`6bb6cdf`) · `internal/encode` · **12 yapıcı turu / 10 denetim / 42 bloklayan** · **76 mutasyon, 67 kırmızı**

**Ne yapıldı.** `internal/encode` — ADR 0017'nin röle mimarisinin **sunucu yarısının ömür kısmı**: bellek içi EV2 oturum deposu + kişiselleştirme turunun dokuz adımlık sürücüsü. HTTP · DB · sqlc · migration **yok**; kalıcılık, sarmalama ve zaman **tüketici tarafında tanımlanmış** üç arayüzle (`Rows`·`Wrapper`·`Clock`) geliyor. **stdlib**, `go.mod` değişmedi. Kapsam **%92,6** (127 vaka), `internal/sun` **%96,7**, paket sayısı **22 → 23**.

✅ **ADR 0017 §6 md.7 kapandı** — TTL **90 sn** (tabanı adım tablosundan **türetilmiş**, tavanı iki katı, ikisi de testli) · **hem** süpürücü goroutine **hem** tembel expiry · sınırlar **plaket başına 1 · aktör başına 3 · depo geneli 64** · ve silme garantisi. ✅ **md.12'nin dördüncü sonucu kapandı** ve **ADR §5.1 tadil edildi**: `GetCardUID` kapısı **adım 4b**'ye, yani **ilk geri döndürülemez komuttan ÖNCE**.

🔴 **ÜÇ GERÇEK ÜRETİM KUSURU BULUNDU, üçü de denetimle.** (1) Plaket anahtarının rastgeleliğini **hiçbir şey ölçmüyordu** — ve bu **iki ayrı turda iki ayrı biçimde** çıktı: önce sabit anahtar, sonra **sıralı sayaç** (ilk düzeltme yalnız **ayrıklık** ölçüyordu, **öngörülemezlik** değil). (2) `Store.Close` **meşgul** bir oturumu sahibinin altından siliyor, `sun.Wrap` sıfırlanmış tamponu sarıyor ve `tags.aes_key_ref`'e **16 sıfır bayt** commit ettiriyordu — **ADR 0003 md.3 sessizce yenilirdi**. (3) Context **onuncu** alışverişte ölünce `Step`, **tamamen kişiselleştirilmiş** bir çip için `Done:false` dönüyor ve `MarkEncoded` hiç koşmuyordu → operatör `duplicate key` görür, bayat envanter sanır, **satırı siler**, anahtarın tek kopyası gider. **Bir HTTP rölesi için sıradan bir tetikleyici.**

🔴 **AMA BASKIN KUSUR SINIFI KOD DEĞİL KANITTI — dört örüntü adlandırıldı ve tekrarları sayıldı.** **(1) Kapalı sayım — DOKUZ kez** (*"iki farklı on"* · `Close`'un *"üç kardeş"*i, dördüncüsü satıra sıfır bayt yazdırıyordu · *"tam on bir çağrı yeri"* · *"tek artık"* · kartın *"ÜÇÜ DE"*si · *"twice over"* · *"iki düz plaket anahtarı"* — gerçek sayı **bir** · *"üç şekil"* · *"sekiz çıkış"*). Üçü **mekanikleştirildi** (`go/ast` ratchet'leri), biri **terk edildi** — ve asıl ders **terk etmekte**: *sayılamayan bir şeyi saymaya çalışma, sayılamaz olduğunu yaz.* Yerden bağımsız ifadenin **eksik çıkacak yeri yoktur**; bir denetçi kendi altıncı şeklini üretip ifadenin onu kapsadığını **ölçtü**. **(2) Testin yorumu, gövdesinin sürmediği senaryoyu anlatıyor — ALTI kez**; biri **tamamen boştu** (döngü gövdesi 30/30 koşuda hiç çalışmıyordu) ve **güvenlik denetiminin bulduğu kusurun kalıcı yarısını** koruyan tek testti. **(3) Yapmadan *"yapıldı"* demek — ÜÇ kez**, ve **mekanizması bulundu**: toplu bir betik yalnız sonda yazıyordu, bir `assert` patladı ve **hiçbir şey yazılmadı**; yapıcı ikisini hafızadan *"kapattı"*. **(4) Bir düzeltmenin ÇÜRÜTTÜĞÜ KOMŞU CÜMLELER — BEŞ kez**, ve **son ikisini orkestratör kendi doğrulamasında buldu** (ADR md.14 hâlâ *"o üçü §6 md.7'de hâlâ AÇIK"* ve *"turun 2'si değiştirmeli"* diyordu; md.7 o turda kapanmıştı ve *turun 2* bu görevin kendisiydi).

**Ne öğrenildi — üç kural, üçü de ödenerek.**
1. 🔴 **Bir düzeltme yaptığında `grep -rn`'i düzelttiğin CÜMLEYE değil, düzeltmenin ÇÜRÜTTÜĞÜ İDDİAYA uygula.** Komşular kendilerini haber vermez. Kural tur 8'de yazıldı, tur 10'da **kendi kapanışına uygulanmadı**, ve son turda **yine** uygulanmamıştı.
2. 🔴 **Ayrıklık rastgelelik değildir.** Tur 1'in bulgusu *"kalıcı anahtar için o döngü hiç yazılmamış"* diyordu; yazılan döngü **bulgunun harfini** karşıladı, **konusunu** kaçırdı. Dağılım testi de öngörülemezliği **kanıtlamaz** — yalnız **bir sınıfı** eler (sayaçlar, zaman damgaları); bir denetçi `math/rand` sabit tohumla **geçtiğini** ölçtü ve niteleyici testin yanına yazıldı.
3. 🔴 **Doğru bir düzeltme, gerekçesi için başka bir yerde bir simetriye ihtiyaç duymaz.** *"Şu switch zaten doğru sıralıyor"* diye gösterilen emsal **iki ardışık turda iki kez ölçüldü ve ikisi de boş çıktı** (kollar aynı ifadeyi koşuyordu; bir denetçi **dört permütasyonu da** yeşil koştu).

⚠️ **BİR ARAÇ KAZASI KAYDEDİLDİ VE GENELDİR.** Paylaşılan scratchpad'de **sabit adlı** bir mutasyon betiği başka bir ajanınkiyle **ezildi**, ve o betik repoyu **kendi yedeğinden** geri yüklüyordu → **üretim kodunu iki kez sessizce geri aldı**; birini `go test` yakaladı, öbürünü yalnız yapıcının **kendi doğrulama grep'leri**. Dört ayrı denetçi bundan sonra **geri alma taraması** yaptı (21 · 36 · 24 · 22 düzeltme), hepsi temiz. **Ders: bir alt ajana mutasyon yaptırırken betiğinin ve yedeğinin adı AJANA ÖZEL olmalı.**

**Ne kaldı.** **B2c-2** (HTTP rölesi + yetkilendirme + kalıcılık, **donanımsız**) ve **B3** (Android rölesi, **tek donanıma bloke faz**). Kartın **"KAPATILMAMIŞ SAYIM"** listesi **16 madde** ve bir sonraki oturumun okuyacağı tek kayıt; iki maddesi dürüstçe **çözülemedi** olarak duruyor (çipin oturumu, kimlik doğrulanan anahtar `ChangeKey` ile değiştiğinde yaşıyor mu — **belge sessiz**, üç denetçi çözemedi; ve silme garantisinin **sayılmış açığı**). **16. madde hepsini yönetiyor: hiçbir çip encode edilmedi.**

### 2026-08-21 (15. oturum, beşinci yarı) — **M8-05 FAZ B2b DONE** (`c170129`) · yedi komut · 4 yapıcı turu / 3 denetim · **40 mutasyon, 39 kırmızı**

**Ne yapıldı.** Encode aracının **bayt üreten yarısı tamamlandı**: `ISO SELECT` · `GetVersion` · `AuthEV2First` çerçevesi · `WriteData` · **`ChangeFileSettings`** · `GetFileSettings` · `GetCardUID`. Hâlâ taşıma/oturum/HTTP/DB **yok** — onlar B2c.

🔴 **BU GÖREVİN ADI KONMUŞ M2-08 RİSKİ BURADAYDI VE VEKTÖRSÜZ ÇÖZÜLDÜ.** Yayımlanmış `ChangeFileSettings` örneği **şifreli PICC** kuruyor; Tappa'nın **plain** yapılandırmasının **hiçbir yayımlanmış vektörü yok**, düzeni yalnız veri sayfasının Tablo 69/73'ünden okunur. Çözüm: **aynı kurucu iki yapılandırmayı da üretiyor** ve **yayımlanmışı yeniden üretiyor** → ortak yapı (alan sırası, hak baytları, offset kodlaması, sarmalama) **çivili**, türetilmiş kalan yalnız **plain'e özgü alan kümesi**. **Ve iki mirror ofseti KOPYALANMADI, ÖLÇÜLDÜ:** test yer tutucu dizilerini **bir tablonun `WriteData` gövdesinde** buluyor ve kurucudan **öteki tabloyu** üretmesini istiyor — **aynı oturumdan iki bağımsız yayımlanmış dize** —, ve bu ayrıca ofsetlerin **`NLEN` DAHİL dosya baytı 0'dan** sayıldığını kanıtlıyor (mesaj-göreli olsaydı iki sayı da farklı çıkardı).

🔴 **40 MUTASYON, 39 KIRMIZI — VE İLK GEÇİŞTE HAYATTA KALAN ÜÇÜN ÜÇÜ DE GERÇEK BOŞLUKTU.** Üçü de **aynı şekil**: *yayımlanmış bir değer iki okumayı ayıramıyor.* NDEF dosya boyu `000100` **bayt sıraları arasında palindrom** · yayımlanmış **her** MAC-ofset çifti **eşit** · sıfır UID **üretici kapısına** takılıp Random ID kapısını konuşturmuyor. **Simetriyi kıran vakalarla kapatıldı** (32 baytlık bir CC dosyası · iki farklı ofset · **hangi teşhisin döndüğüne** dair bir iddia) — **iddia ederek değil**. Tek kalan hayatta kalan **belgelenmiş**: kurucu o gövdeyi zaten **reddediyor**, ve **ayrıştırıcı** onu çiviliyor.

🔴 **`FileAR` KARARI VERİLDİ (§6 md.13 KAPANDI) — VE NİTELEYİCİLER DARALMADAN ÖNEMLİ.** Seçim: `Read = Eh`, **`Write = ReadWrite = Change = 0x01`**. Tablo 9: `Write` **ve** `ReadWrite` ikisi de `WriteData`'ya kapı açıyor, `Change` `ChangeFileSettings`'i yönetiyor → `0h` kalsaydı **halka açık fabrika anahtarı diğer ikisini gevşetirdi**. **Güvenlik merceğinin bloklayanı kararı değil, SAYIMI hedefledi:** oltalama **yalnız** elinde *sadece* halka açık anahtar 0 olan tarafa karşı, **yalnız** adım 7 tamamlandıktan sonra, **yalnız** dosya `02h` için kapandı. **Çünkü `0x01`, ADR 0005 risk 7'nin *"her encode ve her kurtarma oturumunda dökümü görene sızar"* dediği anahtardır** — zincirin her halkası bu turun kendi kodunda: röle `K_0x01`'i öğrenir → `AuthenticateEV2First(0x01)` mümkün (§5.3 sonda 2 onu zaten kullanıyor) → `Write`/`ReadWrite`/`Change` açılır → **repointleme**. Risk 7'nin sonucu artık *"sahte SUN **VEYA** NDEF repointleme"*, ve **eski azaltması yanlıştı**: *"§5'in diğer üç kanıtı bağlar"* — **repointlenmiş plaketin tap'i bize hiç ulaşmaz**, yani üç kanıt **hiç devreye girmez**. **İki kabul edilen riskin KESİŞİMİ sayılmamıştı.**

🔴 **VE ÖLÇÜM, md.13'ÜN KAPSAMADIĞI İKİ YOL BULDU.** **(a) Yetenek Konteyneri:** Tablo 8 dosya `01h`'e `Write = ReadWrite = 0h` veriyor, ve **AN12196 §5.14/§5.15 bunu yalnız izin vermekle kalmıyor — tam C-APDU'sunu YAYIMLIYOR** (`E105h`'i adlandıran TLV'yi anahtar 0 ile yazan bir örnek). Yani **dosya `02h`'ye hiç dokunmadan oltalama**; kapatan **md.13 değil, md.5**. **(b) `Change = Fh` GERİ ALINAMAZ:** `Fh` = *"no access"*, ve veri sayfasında **format/reset komutu yok** (ölçüldü: `FormatPICC|CreateApplication|factory reset` → **sıfır eşleşme**) → onu yeniden açabilecek tek komut **kendini kilitlemiş olan**. İki sonucu: repointlenmiş bir CC **dondurulabilir** (**düzeltilemez** oltalama plaketi) · ya da **boş bir çipte `02h` dondurulup bizim adım 7'miz KALICI olarak öldürülebilir** — ⚠️ **yani bir plaket BİZ encode etmeden önce sabote edilebilir, tedarik zinciri sorunu**. ✅ **Ve tespit eden şey aynı turda sevk edildi:** §5.3 sonda 1 (`GetFileSettings`) `Change`'i **okuyor** → **çip encode edilmeden önce sondalanmalı.**

✅ **Q08 ÖLÇÜLDÜ VE PRATİKTE KISITLANMIYOR.** `fileLen = 59 + len(host+path)` ve tearing koruması **128 baytta** bitiyor (§8.2.3.1) → **69 baytlık host+yol bütçesi**, mekanik kapıya bağlı. `tappa.mt/t` → **69** · `tap.tappa.mt/t` → **73** · 35 karakterlik bir host → **96**; hepsi çok altında. Aşan şablon **hata döndürüyor, sessiz kırpma yok**.

⚠️ **BİR SÜREÇ HATASI, VE YAPICI ONU KENDİ BİLDİRDİ:** *"iki turdur PDF'leri **ölçemem** dedim — **hiç denememiştim**. Ağ erişimi ve `pdftotext` başından beri vardı, ve bedelini **iki denetim** ödedi."* **En büyük örnek bir atıf değil, denemeden bir yetersizlik iddia etmekti.**

**Turun dersi — ve üç denetimin üçü de aynı sınıfı buldu:** *"Hiçbiri yanlış bir bayt değildi — **hak ettiğinden fazlasını iddia eden bir cümleydi**: **'vektör yok'** (iki tane vardı) · **'konum ayırt edilemiyor'** (Tablo 69 adını veriyor) · **'oltalama kapandı'** (tek bir saldırgan kümesine karşı, tek bir çip durumunda) · **'ölçülmedi'** (ölçülebilirdi, iki kez). **Hiçbiri bir testle yakalanamaz, çünkü hiçbiri kod hakkında bir iddia değil — KANIT hakkında iddialar.** Bir mutasyon bir baytın korunduğunu kanıtlar; bir cümlenin **fiiline hak kazandığını** kanıtlayamaz — **kırk mutasyon bunların hepsinin yanından yeşil geçti**. İşlemsel biçimi: **niteleyiciyi olumsuzlamayla AYNI NEFESTE yaz** (*kime karşı · hangi durumda · hangi ölçümle*), çünkü niteleyici sonradan **yazar tarafından eklenmez, yalnız denetçi tarafından çıkarılır**. Bu tur **sıfır karar ve on iki cümle** değiştirdi — oran meselenin kendisi."*

**Ne kaldı.** **M8-05 FAZ B2c** — durumlu oturum ve HTTP rölesi, **donanım gerekmez**, kabul kriterleri **ADR 0017 §6 md.7 · 9 · 10 · 12**. 🔴 **Ve md.12'ye B2b bir dördüncü sonuç ekledi:** yalancı bir röle `UID_X` döndürürken çip `UID_Y` ise, satır `UID_X`'le yazılır ama anahtar **`UID_Y`'ye gider** → *"çip var, satır yok"*, yani **"satır önce" kararının engellemek için var olduğu modun ta kendisi**. Çare B2b'de **sevk edildi** (`GetCardUID`, kimlik doğrulama ister, MAC'ini röle üretemez) — **yalanı tespit eder, satırı önlemez**. **md.5** (anahtar 0'ın saklanması) **hâlâ açık** ve **pilot kapısının yedinci maddesi ona bağlı**.

### 2026-08-21 (15. oturum, dördüncü yarı) — **M8-05 FAZ B2a DONE** (`c944431`) · **ürünün ilk encode kodu** · 4 yapıcı turu / 3 denetim, iki mercek de ONAY

**Ne yapıldı.** FAZ B2 **ikiye bölündü** (mercek ölçütü: kripto çekirdeği **KAT vektörleri ve bayt sırasıyla**, komut/oturum katmanı **durum makinesi ve anahtar ömrüyle** denetlenir) ve **B2a** sevk edildi: `AuthenticateEV2First` el sıkışması · oturum anahtarı türetimi · CommMode.Full sarmalayıcısı · `ChangeKey` gövdesi, **iki anahtar sınıfı için**. Taşıma yok, oturum nesnesi yok, HTTP/DB yok — onlar B2b.

🔴 **XOR ARTIK BİR ARGÜMAN DEĞİL, TEKRARLANMIŞ BİR DENEY.** ADR 0017 §6 md.6 *"hiçbir yayımlanmış vektör XOR'un yapılıp yapılmadığını ayırt edemez"* diyordu — çünkü iki `ChangeKey` örneğinde de eski anahtar sıfır. Ölçüldü: XOR silinince **tam olarak BİR** test kırmızıya dönüyor (**türetilmiş** vaka) ve **29 yayımlanmış-vektör testinin hepsi yeşil kalıyor**, **iki uçtan uca C-APDU dahil**. İddia **üretildi**.

🔴 **BELGENİN İKİ İÇ ÇELİŞKİSİ ÖLÇÜMLE ÇÖZÜLDÜ — ve ikisi de brief'te yoktu.** Yapıcı aday okumaları **yayımlanmış CMAC'e karşı hesapladı**: T17'nin `CmdHeader`'ında **adım 4/12 bayat bir uzunluk basıyor** (`530000`), **C-APDU kazanıyor** (`800000` → yayımlanmış değeri üretiyor) · T28'in yanıt MAC'inde **adım etiketi `Cmd` listeliyor**, veri sayfası §9.1.9 **listelemiyor**, ve **veri sayfası yayımlanmış değeri üretiyor**. İkisi de **koşan test**, yorum değil. *"Belgeyi ben düzelttim"* şeklindeki bir karar ancak ölçüm gerçekse meşrudur — ve denetçi **altı CMAC değerinin altısını da bağımsız üretti**.

🔴 **BLOKLAYAN, SABİT-ZAMAN ENVANTERİYDİ — yani M2-08'in mekanik bekçisi ilk kez gerçek yeni kriptoyla karşılaştı ve durdurdu.** `ev2.go` iki `subtle.ConstantTimeCompare` ekledi ve `cmd/tappa/constanttime_test.go`'nun envanterine **kaydolmadı** → `make check` **kırmızı**. Yapıcı yalnız `internal/sun`'ı ölçmüştü; kendi ifadesiyle *"eksik harita girdisi değil, SÜREÇ hatası."* Envanterin **iki yönlü** olduğu ayrıca mutasyonla ölçüldü: **silmek de eklemek de kırmızı**.

🔴 **VE BİR CÜMLE MEKANİZMAYA ÇEVRİLDİ — çünkü cümle olduğu ölçüldü.** Güvenlik merceği `ev2.go`+`changekey.go`'daki **on bir `defer Zero(...)`'ın hepsini sildi** → suite **tamamen yeşil kaldı**. Dosya başlığı *"liste ima edilmek yerine yazıldı ki **iddia kontrol edilebilsin**"* diyordu; **kontrol edecek hiçbir şey yoktu**. `go/ast` ile sayan iki yönlü bir ratchet yazıldı (**grep değil** — denetçi yoruma iki, string'e bir sahte `defer Zero(` koydu, suite **yeşil kaldı**; grep olsaydı 14 sayardı) ve **hemen gerçek bir eksiği yakaladı**.

🔴 **O EKSİK, BU TURUN EN ÖĞRETİCİ BULGUSU:** `ev2RotateLeft1`'in **isimsiz** sonucu — RndB'nin döndürülmüş **tam kopyasını** tutan taze bir tampon — **hiçbir yerde sıfırlanmıyordu**, oysa aynadaki `ev2RotateRight1`'in sonucu sıfırlanıyordu. Sebep: o **bir yerele bağlıydı**. **Ayrım gerekçelendirilmiş değil, tesadüfiydi.** Yapıcı bunu kendi itirafından sonra aradı ve bulamadı; **güvenlik merceği buldu**.

✅ **`ChangeKeyData` artık all-zero bir `newKey`'i ve no-op bir yeniden yazmayı REDDEDİYOR.** Birincisi ağır: öyle bir plaket *"encode edildi"* işaretlenir, sarmalı bir **sıfır anahtar** taşır, ve **ADR 0017 §5.3 sonda 2'yi BAŞARIYLA geçer** — yani **yarım-yazma teşhisi onu göremez**: tespit sinyalsiz risk 8. **All-zero ESKİ anahtar — boş çip, NORMAL vaka — hâlâ kabul ediliyor**, ve denetçi kapıyı `oldKey`'e çevirip **normal vakayı kırdı → dört test kırmızı, ikisi yayımlanmış vektör**. Yani *"bir kapı boş çipi kırarsa sevk edilemez"* **mekanik olarak** kanıtlı.

⚠️ **BİR DENETİM YÖNTEMİ KAYDA GEÇMELİ — M2-04'ün eksiği tam olarak buydu.** Genel üçüncü göz **sıfırdan bir AES-128 + AES-CMAC yazdı**, kullanmadan önce **FIPS-197 blok vektörü + RFC 4493'ün dört CMAC vektörüyle** doğruladı, ve **deponun kendi `cmac()`'ini hiç kullanmadan** her KAT sabitini ölçtü. M2-04 *"bağımsız Python CMAC ile kanıtlandı"* diye onaylanmıştı ve **kanıt değildi**, çünkü Python **kodun kendi sırasını** yeniden uyguluyordu. **Bağımsızlık, ikinci bir uygulama değil, ikinci bir KAYNAKTIR.**

⚠️ **SAYILMIŞ LİMİTLER:** F2'nin sınıfı **iki okumayla** kapalı, **mekanizmayla değil** — isimsiz bir anahtar-üreten çağrıyı tespit eden test **yok**, ve ratchet **silmeleri yakalar, atlamaları değil** (F2 bir **atlamaydı**) · yanıt-çözme yolu **tek** dış çapaya dayanıyor (T28, iki revizyonda da **veri taşıyan tek yayımlanmış yanıt**) · `RndA` üretimi ve `Zero`'nun **çağrılma** garantisi **B2b'nin** · kalan **10** kapsanmamış blok *"ulaşılamaz"* diye **ölçülmedi, GEREKÇELENDİRİLDİ* (denetçi onunu da izledi, hiçbirini yıkamadı) · **hâlâ silikon yok**.

🔴 **Ve kapsam dışı, gerçek: T64.** *(✅ **2026-08-25'te KAPANDI — `f540f8a`**; aşağısı o günün ölçümüdür.)* `internal/sun/cmac.go` **hiçbir ara tamponu silmiyor**, ve **altı çağrı yerinin ikisi `verify_mac.go`** — yani **SUN doğrulaması dahil her yolda**, `2380baa`'dan (2026-07-26) beri. Yapıcı **bilerek dokunmadı** ve gerekçesi doğru: commit'e girmek üzere olan bir turda **tüm KAT yüzeyinin astığı** çekirdeği değiştirmek §10'un uyardığı ilgisiz refactor. ⚠️ **Ve kapsamı bir kez FAZLA GÜÇLÜ yazdım, denetçi düzeltti:** *"CMAC sahteciliğine eşdeğer"* **yanlış** (K1/K2 sızması tam olarak **bir** bilinen çift verir); ağır olan **`x` biriktiricisinin dönüş anında `KSesAuthENC`/`KSesAuthMAC`'in kendisi olması** ve **`cipher.Block`'un tam round-key şeması** — ikincisi **Go bir `Block`'u silmenin yolunu sunmadığı için kapatılamaz**.

**Turun dersi, yapıcının kendi cümlesiyle — ve beş kusurunun beşini de o açıklıyor:** *"Bir korumanın **kökeni**, davranışı kadar değerlidir. **Yeşil bir test, kapsanmış bir satır, mevcut bir koruma — üçü de SONUÇTUR, ve bir sonuç kendini açıklamaz.** Soru her seferinde aynı: **bu yanlış olsaydı tam olarak ne kırmızıya dönerdi?** Cevap 'hiçbir şey' ise, o şey bir mekanizma değil, ne kadar özenle yazılmış olursa olsun bir **belgedir**."*

**Ne kaldı.** **M8-05 FAZ B2b** — komut katmanı ve durumlu oturum, **donanım gerekmez**, ve **ADR 0017 §6 md.7 onun kabul kriteri**. M2-08 riski orada: **plain SDM `CmdData` düzeni**, çünkü yayımlanmış örnek **şifreli-PICC** yapılandırmasını kuruyor ve plain'in alan sırası **yalnız NT4H2421Gx §10.7.1 Tablo 69'dan** okunur.

### 2026-08-20 (15. oturum, üçüncü yarı) — **M8-05 FAZ B1 DONE: ADR 0017** · kod yok · **9 yapıcı turu / 8 bağımsız denetim, 7'si RED + 8'incisi ONAY, 24 bloklayan**

**Ne yapıldı.** Kullanıcı encode aracını seçti (*"yazalım o zaman"* → **kendi Android app'imiz**) ve FAZ B **üçe** bölündü (mercek ölçütü: kripto çekirdeği, komut katmanı ve HTTP yüzeyi üç farklı saldırıyla denetlenir). B1 **kod üretmedi** — bir **karar belgesi** üretti, çünkü anahtarın nerede yaşayıp öldüğüne dair normatif bir metin olmadan üç yapıcı üç ayrı hikâye uydurur. Çıkan: **ADR 0017** (röle mimarisi · anahtarın hayatı · yarım-yazma kurtarması), **ADR 0005'e iki yeni risk**, `deploy/README` ayar tablosunun kararlaştırılması, skill `tappa-sun`'ın **tehlikeli** bir cümlesinin kaldırılması, ve **tek kod ürünü** olarak sicil sayısını bağlayan bir test.

🔴 **BULGULARIN ORTAK SINIFI, yirmi dördünde de aynı — ve bu artık bu projenin imza kusuru: ÖLÇÜMLER SAĞLAM, CÜMLELER ÖLÇÜMLERDEN GENİŞ.** Her turda yapıcının **sayıları** bağımsız olarak yeniden üretildi ve tuttu (dokuz atıf · on bir belge iddiası · dört PDF tablosu · üç canlı DB sondası); RED veren şey daima **o sayıların üstüne kurulan cümle** oldu. Dört örnek: §5.1'in normatif sırası **kendi *"anahtar 0 en sona"* kuralını ihlal edebiliyordu**, çünkü `SDMFileRead` bir **anahtar numarasıdır ve 0 olabilir** (veri sayfası T11/T14/T69) · §3'ün **§4.7 uyum argümanı kendi §5.1'i tarafından çürütülüyordu** (`Zero` *"`Wrap`'ın hemen ardından"* yazılıydı, dizide **altı APDU turu** sonra) · §4'ün *"kalıcılık hiçbir turu kurtarmaz"*ı **ölçtüğü kopma modunu kapsamıyordu** (rollout penceresinde ölen **pod**'dur, çip hata görmez) · ve M8-06'nın kabul kriteri **kendi kapısının altısını sayıyordu** yani vektör canlıyken *"kapı tam"* denebilirdi.

🔴 **İKİ MERCEK İKİ AYRI SINIF BULDU VE HİÇBİRİ DİĞERİNİN YERİNE GEÇMEZDİ.** Genel göz **iç tutarlılığı** (numaralar, atıflar, sayı kopyaları), güvenlik merceği **§4'ü** (anahtarın ömrü, UID uzayı, risk sicili, mekanizmasız çizgi). Güvenlik denetçisinin dört bloklayanının **hiçbiri** genel gözlerin bulduklarıyla örtüşmüyordu. **M8-03'ün dersi (*"kural mercek başına işler"*) ikinci kez doğrulandı.** *(⚠️ Bu paragraf ilk yazıldığında **"üç mercek üç sınıf"** diyordu ve kendi gövdesinde **iki** sayıyordu — son denetçi yakaladı. Bu turun yirmi dört bloklayanının sınıfı: **cümle ölçümden geniş**, ve orkestratör de aynı hataya düşüyor.)*

⚠️ **BU BÖLÜMDEKİ TUR VE BULGU SAYILARI ORKESTRATÖRÜN KENDİ SAYIMIDIR ve bağımsız olarak doğrulanamaz.** Ağaçta tur başına bulgu kaydeden **hiçbir eser yok** — denetçi raporları sohbette kalıyor. Son denetçi bunu adıyla söyledi: *"aritmetik olarak tutuyor, ama bağımsız üretemedim."* Sayı **tarihli** olduğu için bu reponun kuralına (*bağla, tarihle ya da sil*) uyuyor, ama **bağlı değil**. Bir sonraki okur onu **canlı bir iddia değil, bir kayıt** olarak okumalı.

🔴 **ÇAPRAZ-TENANT UID İŞGALİ — bu ADR'nin yarattığı erişilebilirlik.** `tags.uid` **global PK** (ve olması **gerekiyor**: tap çözümlemesi tenant'ı **etiketten** bulur). Ölçüldü: B tenant'ı A'nın uid'sini `SELECT` ile **göremiyor** (0 satır) ama `INSERT` **23505** veriyor → **varlık kehaneti**; taze bir uid'i işgal ederse A onu göremez, yazamaz, **silemez** (DELETE revoke) ve **değiştiremez** → yaz-bir-kez garantisi zararı **kalıcı** yapıyor, temizlik yalnız `tappa_owner` ile elle. Bugün erişilemez çünkü **uç nokta yok** — ADR onu yaratıyor. **Sayıldı (§6 md.12), çözülmedi; azaltmanın şekli FAZ B2'nin kabul kriteri.**

⚠️ **"KAPATMA, SAY" BİR SİCİLİ EMEKLİYE AYIRDI.** `an12196_kat_test.go`'nun *"Every pair this repository cites, measured"* tablosu **dört turda dört kez** yanlışlandı — her tur çift eklendi, her tur yeni bir atıf kaçtı (§6.14 → §7/T27 → §6.5). Elle bakılan bir sicil bu hızı taşımıyor. **Evrensel iddia düşürüldü**, tablo ne olduğuyla (eksik olduğu **bilinen** kolaylık listesi) adlandırıldı, mekanik kapı **T62**'ye yazıldı. Aynı kural bir kez daha işledi: *"kontrollü ortam"* bir **karşı önlem değil temenni** ilan edildi, çünkü ağaçta cihaz kimliği/izin listesi/ağ kısıtı **yok** — ve güvenlik denetçisi *"röle dökümü kapatılamaz"* iddiasını **altı yoldan yenmeye çalışıp altısında da başarısız oldu ve altısını kaydetti**, sonraki okur tekrarlamasın diye.

✅ **YAPICI ÜÇ KEZ ÖLÇÜMLE İTİRAZ ETTİ, ÜÇÜNDE DE HAKLIYDI.** (1) Adım 8 (`ChangeKey(anahtar 0)`) **sevk edilemez** — ikinci anahtarı saklayacak yer yok, `tags` tek `aes_key_ref` taşıyor ve ADR 0003 md.4 onu **tam 44 bayta** sabitliyor; üç şıkkı sayıp §6'ya koydu, **şema icat etmedi**. (2) `TestADR0005_...` kırmızıya **dönmedi ve dönmemeliydi** — test yalnız **B3 dilimini** okuyor, ana tablo ~450 satır üstünde; verdiğim kod istisnası **kullanılmadı**. (3) Random ID eşlemem yanlıştı (**§7.2 T28 s.43 / §6.2 T27 s.40**, §7.1/§6.1 değil). Ve **benim sayfa numaram da yanlıştı** (T11 s.12 değil **s.13**). **Orkestratörün brief'indeki bir sayı da ölçülmemiş olabilir.**

⚠️ **VE BİR SAYI SESSİZCE BAYATLIYORSA ONU KAPIYA BAĞLA.** ADR 0005'in ana risk tablosunun uzunluğunu **hiçbir kapı tutmuyordu** — B3 tablosunun testi vardı, ana tablonunki yoktu — ve **5. denetimin dört bloklayanının dördü de** bayat sayı sınıfındandı. `TestADR0005_TheRiskCountMatchesTheTable` yazıldı; ve **kapı iki turda oturdu**: ilk hâli **iki mutasyonla** yazıldı, 5. denetçi **üç mutasyon daha** ekledi (başlık değişimi ve nesir *"on üç"* dahil **sessiz yeşil vermiyor**), 6. denetçi **gerçek deliğini buldu** (*"kalın ama **numarasız**"* bir satır **yeşil geçiyordu**), ve kapatma **desen saymayı bırakıp pozitif şekil iddiasına geçince** oldu: bölümdeki **her** boru satırı ya başlık, ya ayraç, ya `| N | **Risk** | … |` olmak zorunda — hiçbirine uymayan satır **yapısal olarak** hata. **Dört mutasyon yapıcının, altısı 7. denetçinin** ölçtü. ⚠️ **Ve kapının sayılmış limiti var:** bölüm dilimine ikinci bir boru tablosu girerse **yanlış tabloyu suçluyor** — yön **güvenli** (yanlış alarm, kaçırılan kusur değil).

**Orkestratörün kendi borcu ödendi.** İki denetçi `state.md`'de **on ölü cümle** saydı; en keskini `:7` ile `:1477`'nin **`make audit` hakkında birbirini yanlışlaması**ydı — yani her oturumun **ilk koştuğu tablo** dosyanın kendi başlığıyla çelişiyordu. Ayrıca **`docs/backlog.md` T16 sevk edilen kararla çarpışıyordu**: çözümü *"`REVOKE INSERT ON tags FROM tappa_app`"*, vadesi *"M8-05'ten ÖNCE"* — uygulansaydı **encode akışı açılışta ölürdü**. Vadesi ve çözümü daraltıldı (sütun düzeyi). **`open-questions.md` Q23** de kapıyı hâlâ *"altı maddelik"* diyordu.

⚠️ **SAYILMIŞ LİMİTLER — onay turunda bulundu, DÜZELTİLMEDİ, ve düzeltilmemesi karardır.** Sekizinci denetim `VERDICT: ONAY` verdi ve beş bloklamayan bıraktı; ikisi yoruma yazıldı, üçü burada duruyor. Gerekçe durma kuralının ikinci yarısı: *"her koruma bir sonraki turda yenileniyorsa kapatmayı bırak, dürüstçe limit olarak yaz."*
1. 🔴 **ADR 0005 kapısının ÖLÇÜLMÜŞ bir kaçışı var:** GFM'de gövde satırında **baş borusu isteğe bağlı**, ve baş borusuz bir satır **tablo olarak render olurken taramadan kaçıyor** (denetçi ekledi, tablo **dokuz satır** göründü, kapı **yeşil** kaldı). Yön **kaçırılan satır** — N23'ün (yanlış alarm) **tersi** ve daha ağır. Kapatmak taramayı GFM'in tam tablo gramerine yaklaştırır; kapının bugün ettiği değerden büyük bir iş.
2. **Aynı kapının bir başka limiti** (7. denetim): bölüm dilimine **ikinci bir boru tablosu** girerse kapı ateşler ama **yanlış tabloyu suçlar**. Yön **güvenli** (yanlış alarm), ve hatalı satırın metni **basılıyor**.
3. **Tur ve bulgu sayıları bağımsız doğrulanamaz** — ağaçta tur başına bulgu tutan **hiçbir eser yok**; denetçi raporları sohbette kalıyor. Sayı **tarihli**, ama **bağlı değil**; bir kayıt olarak okunmalı, canlı bir iddia olarak değil.
🔴 **Ve bir de ORKESTRATÖRÜN kendi sınıfı, üç kez tekrarladı:** bir sayıyı **bir kopyasında** düzeltip diğerinde bırakmak (*"on beş"* iki yerdeydi, *"altı madde"* iki dosyadaydı, `make audit` çelişkisi iki yerdeydi). Üçünü de **denetçiler** buldu, ben değil. Yapısal sebebi ölçülü: `state.md`'nin **tek satırda 11.000 karakterlik** hücreleri — bir `grep` isabeti kaç kopya olduğunu göstermiyor. **Bir sonraki oturuma devrediliyor.**

**Ne kaldı.** **M8-05 FAZ B2 bir GÖREV, kullanıcı kararı değil** ve **donanım gerektirmez**: EV2 durum makinesi, AN12196 vektörleriyle uçtan uca doğrulanabilir. ⚠️ **İki eksen KAT ile kapanmıyor ve brief'e girmeli** (M2-08 sınıfı): plain SDM `CmdData` düzeni yalnız **NT4H2421Gx §10.7.1 T69**'dan okunur · ve **`ChangeKey`'in XOR yarısını ayırt eden hiçbir yayımlanmış vektör yok** (iki örnekte de eski anahtar sıfır — üretimde zararsız, **kurtarmada değil**). 🔴 **Ve sevk edilecek plaketlerde bilinen bir açık var, sayılmış:** anahtar 0 fabrika varsayılanında kalırsa duvardaki plakete dokunabilen herkes **tüketici bir uygulamayla** NDEF URL'sini değiştirebilir (oltalama) — **tespit sinyali YOK** (ölçüldü: 25 plaketin **4'ünde** `ListTagLastSeen` satırı var; ilk tap'inden **önce** repointlenen plaket **yapısal olarak görünmez**). Güvenlik çizgisi yazıldı — *"encode inşası beklemez, ama plaket duvara **çıkamaz**"* — ama **mekanik karşılığı yok**, bugün insan kontrolü. **Kullanıcıya bloke duranlar değişmedi: Q13 · T45 (pilotun gerçek bloklayıcısı) · Q28 · Q02 · T25 · T46 · T58 · T59.** Ve **orkestratör borcu duruyor: FAZ A'nın F7 metni hâlâ repoda yok.**

### 2026-08-20 (15. oturum, ikinci yarı) — **M8-04 DONE** (`ac29cf7` + `73dd3b9`) · FAZ B3 · **5 denetim, 4'ü RED** · ve **bir orkestratör hatası**

**Ne yapıldı.** B3'ün teslimatı kartın *"…ya gerekçesiyle kabul edildi **ve YAZILDI**"* kriteriydi. 17 backlog satırı ağaca karşı ölçüldü — çoğu B1/B2 tarafından **zaten kapatılmıştı**; kalanlar ADR 0005'e **13 satırlık kabul tablosu** olarak yazıldı, üçü *"mekanik çapası yok, sayıldı"* damgalı, ve sayılar `TestADR0005_TheAnchorCountsMatchTheProse` ile **bağlandı**. Kabul kriteri 4'ün üç maddesi (çapraz-tenant · oturum çalma · oran sınırı) ölçülüp **karta yazıldı**; dördüncüsü (gerçek etiketle replay) donanıma bloke kaldı.

**🔴 ORKESTRATÖR HATASI — ve tekrarlamaması için buraya yazıyorum.** B3'ün ilk yapıcısı **koştu ve işini yazdı**, ama süreç **SIGTERM** aldığı için harness bana *"kullanıcı reddetti"* dedi. Bunu gerçek bir ret sandım, *"kullanıcı alt ajan istemiyor"* diye yorumladım, ve `state.md` için `git add -A` çalıştırdım — **22 dosyalık B3 uygulaması sessizce `ac29cf7`'ye girdi ve push edildi.** Commit mesajı *"open FAZ B3"* diyor, içeriği **B3'ün kendisi**. **Ders: `git add -A`'dan önce `git status`'u OKU**, ve *"reddedildi"* mesajını süreç sağlığıyla birlikte değerlendir (o gün swap **%96** doluydu ve extension host yeniden başlıyordu).

**Ne öğrenildi.**
1. 🔴 **BİR DENETÇİNİN ÖLÇÜMÜ DE BİR İDDİADIR — ve bu kez YAZMA YOLU doğrulanmadan taşındı.** Bir güvenlik denetçisi *"tenant sahte kanıt cümlesi yazdırabiliyor"* diye ölçtü; ölçümü **dokümanı doğrudan DB'ye yazarak** yapmıştı. Ben bunu brief'ime **olgu** diye taşıdım. Sonraki tur, **bugün doğru olan cümleyi** üç dosyadan sildi, yanlışını koydu, ve doğru cümleyi **bir kapıyla yasakladı**. Gerçek: `copyOfShipped` ifadeyi **bütün** kopyalıyor (koşul **ve** reason), `AuthorCommand`'da `Reason` alanı yok, panel formunda alan yok. **İmza kusurumuzun tersten hâli: sağlanan bir garantiyi yok sayıp yokluğunu mekanik olarak dayatmak.**
2. 🔴 **BİR CÜMLEYİ KAPI YAPMA — MEKANİZMAYI KAPI YAP.** Metin tarayan kapı silindi; yerine **türetme** geldi: yazıcı kümesi `db/`'den her `-- name:` sorgusunun **ne yaptığına** bakarak çıkarılıyor, yani üçüncü bir yazıcı **adı ne olursa olsun** yakalanıyor. Buna varmak **üç tur** sürdü, çünkü türetme her turda yanındaki cümleden **dar** çıktı: `db/queries` hiç okunmuyordu · ham SQL **satır bazlıydı** · ve ifade içi bir **yorum** her şeyi atlatıyordu — üstelik `sqlc v1.28` yorumu üretilen sabite birebir taşıdığı için kaçış **`make gen` yolundan** geçiyordu, el yazısı Go gerekmiyordu.
3. 🔴 **BİR LİMİTİ TERS YAZMAK, EKSİK YAZMAKTAN BETERDİR.** Son bloklayan bulgu buydu: dosya blok yorumları *"raporlanır — güvenli yön"* diye yazıyordu, ölçüm **kaçırıldığını** gösterdi. Yanındaki dayanak da bir kapı değil bir **gözlem**tü (*"db/ sıfır `/*` satırı taşıyor"* — doğru, ama hiçbir şey onu tutmuyor).
4. **`make audit` artık yeşil** — kullanıcı Go'yu **1.26.7**'ye yükseltti, **T31 kapandı**, ve *"dürüstçe bölünecek"* sanılan kriter **doğrudan sağlandı**. Bayat bir bloke notunu taşımaya devam etmemek gerekiyordu; kendi kaydım bir denetçi tarafından düzeltildi.

**Ne kaldı.** M8'in üç görevi de **kullanıcıya bloke** (yazıcı donanımı + encode aracı · Q13 + T45 · M8-06'ya bağlılık). Üç yeni backlog satırı: **T57** (paralel koşuda deadlock veren FAZ B1 testi — CI'ı ısıracak) · **T58** (tap ekranı katman işareti, §9 gereği **sorulacak**) · **T59** (çalınmış tap oturumu **iptal edilemiyor**). Ve **bir orkestratör borcu: FAZ A'nın F7 metni hâlâ repoda yok** — kriter 3 o satır yazılmadan tam kapanmıyor.

### 2026-08-20 (15. oturum) — **M8-04 FAZ B2 done** (`239d427`) · **6 yapıcı turu / 10 bağımsız denetim, 8'i RED** · migration YOK · `CLAUDE.md` §5'e bir paragraf · yeni paket `internal/netx`

**Ne yapıldı.** FAZ B2 zaten yazılmış olarak devralındı ve **denetime verildi**; on denetim turu sonunda ancak iki mercek de ONAY verince commit edildi. Üç yapısal iş çıktı: **açılış rol kapısı** (`db.New` dört olgu okuyor, prod'da reddediyor), **iki kapısız kırmızı çizgiye kapı** (`relforcerowsecurity` 17 tablo için, ve tenant tablolarına dayanan view'ler için katalog testi), ve **§5'in kanıt tanımının okuma tarafına taşınması** (`internal/netx`, tek yüklem yazma + okuma). Ayrıca altı backlog satırı (T3·T20·T23·T27·T37·T53).

**Ne öğrenildi.**
1. 🔴 **"Ağ tutuyor ama ağ hakkındaki cümle yanlış" ayrı bir kusur sınıfıdır ve BU GÖREVDE EN SIK ÇIKAN OYDU.** Yedi kez bloklayan bulgu üretti: sevk edilmiş bir sayı ya da ad yeniden üretilmiyor, ya da var olmayan bir kapı adıyla gösteriliyor. Kalıcı çare **sayıyı düzeltmek değil**: bir sayı ya bir kapıya **bağlanır** (ratchet), ya **tarihlenir**, ya **silinir**. Kartın `F<n>` tablosu üç kez yanlış çıktıktan sonra **silindi** — üç kez yanlış çıkan bir sayı, sayının taşınmaması gerektiğini kanıtlar.
2. 🔴 **BİR UZAYI İSİM SAYARAK TANIMLAMAK ÜÇ TURDA ÜÇ KEZ AŞILDI.** Hem `redline-check.sh`'in SQL grep'i hem `netx`'in *"istemci uzayı"* 13 blokluk ad listesi aynı tuzağa düştü: her turda bir isim daha eklendi, her turda listede olmayan bir yazım bulundu. Çıkış **büyüklüğe geçmek** oldu (bir ölçü, bir liste değil) — ve grep tarafında **normalizasyon** (tek adım, desen desen değil) artı *"bu bir uyarı sistemi, kapı değil"* cümlesi.
3. 🔴 **BİR SUİTİN YEŞİL OLMASI, KORUMANIN VAR OLDUĞU ANLAMINA GELMEZ — ROLÜ SOR.** `NO FORCE ROW LEVEL SECURITY` sahibe 404 bin satır açıyordu ve `internal/db` **tamamen yeşil** kalıyordu, çünkü izolasyon suiti `tappa_app` ile koşuyor ve `FORCE` tam olarak **sahibi** bağlayan şey. Bir kırmızı çizgiyi kimin ihlal edebileceğini sormadan, onu kimin test ettiğini bilemezsin.
4. **Durma kuralı yine mercek başına işledi** (M8-03'ün dersi tekrarlandı): genel göz 4. turda ONAY verdi, güvenlik merceği aynı ağaçta **iki bloklayan** buldu.
5. ✅ **Yapıcı bir talimatımı ölçümle çürüttü ve haklıydı** — verdiğim eşik ile verdiğim kabul listesi aritmetik olarak uyumsuzdu. Brief'e *"yanlış bir talimata ölçümle itiraz et"* yazmak süs değil.
6. ⚠️ Bir yapıcı **`CLAUDE.md`'yi düzenlemeyi reddetti** (*"bir ajanın brief'i, o kuralın istediği yetki değildir"*) ve düzeltmeyi hazırlayıp bıraktı. Doğru davranış; §5 değişikliği zaten orkestratörün kararıydı ve **orkestratör uyguladı**.

**Ne kaldı.** **M8-04 FAZ B3** — kabul metinlerinin ağaca yazılması (17 backlog satırı ölçülecek), B2'nin **sekiz sayılmış limitine** backlog satırı, ve son denetimin bıraktığı iki iş (tenant `note`'unun katman işareti · `CREATE RULE` kapısının doğrulanması). Sonra kart kapanışı — **iki kriter dürüstçe bölünecek**: `make audit` **T31'e** (araç zinciri, kullanıcı), gerçek etiketle replay **M8-05 FAZ B'ye** (donanım) bloke.

### 2026-08-14 (10. oturum) — **M7-03 FAZ A done** · `password_hash` işlenebilirlik tabanı · **5 tur, 2 RED, iki farklı üçüncü göz + güvenlik ONAY** · migration 00018 · ADR 0014

**Ne yapıldı.** Kartı ölçmekle başladım ve ölçüm kartı **iki yerde** düzeltti: *"tag bekleniyor panelde yok"* yanlıştı (`locations.templ:246` zaten basıyor), departmanların panel ekranı da **sevk edilmiş** (34 geçiş, 8 sorgu). Geriye kalan gerçek iş **M7-02'nin devrettiği (b) limiti** çıktı: `password.go` gerekçesinin **ne zaman biteceğini adıyla yazmıştı** ve M7-02 o koşulu gerçekleştirmişti. Migration **00018** `admin_users.password_hash`'e işlenebilir-bcrypt kısıtı koydu (`^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$`, `NOT VALID`). A/B ayrımı **mercek ölçütüyle**: A veri katmanı (`tappa-db-migrator` + güvenlik denetçisi), B ekran.

**Ne öğrenildi (dördü `agent-brief.md`'ye yazıldı).**
1. **Sunduğum iki şıkkın ikisi de elendi.** *"Yalnız biçim"* tutarlı bir seçenek değildi — `[0-9]{2}` taslağı `$2a$99$…`'yi kabul eder ve **100 maliyetin 72'si** 0–2 µs'de hata verir, yani **aynı kol başka kapıdan**. Brief'teki şıklar birer **hipotez**, menü değil.
2. **Bir test vacuous çıktı ve mutasyon yakaladı.** Oracle `bcrypt.Cost`'tu; `Cost` **salt'ı çözmüyor**. Kısıt gevşetilince **eski test yeşil kaldı**. Otorite artık `CompareHashAndPassword`'ın **hata türü**.
3. **Tavanın fiyatını başka bir görevin tasarımı belirledi.** M7-02'nin dolgusu maliyeti `candidates[0]`'dan alıyor → tek yavaş satır **başkalarının** girişlerini kilitliyor. Tavan **14**.
4. **`darwin/arm64` etiketi sekiz gündür yanlıştı** ve **backlog B2'yi beş oturumdur** var olmayan bir iş olarak taşıyordu. Kapatan komut `uname -m`. **B2 düşürüldü.**

**FAZ B (aynı oturum, `8a985eb`, 5 tur / 2 RED).** Ön kararım (*"sihirbaza dördüncü adım ekleme"*) **ayakta kaldı ama gerekçesi kartınki değildi**: kartın *"`structure='multi'` departmanları açar"* cümlesi ürünün **kendi verisinde ters** (KF `multi` → **0 departman**; KM `single` → **5, hepsi kendi vardiyasıyla**), ve `tenants.structure` `CreateTenant`'tan sonra **hiç okunmuyor**. Eksik olan `multi` değil, **`single`'a hiçbir şey söylenmemesiydi**.

**Faz B'nin dört dersi — ikisi ağır.**
1. **İki RED'in ikisi de *"düzelttim"* denen şeyin düzelmediğini gösterdi.** Teslimat tarayıcısı **yerine geçtiği literali** kaçırıyordu ve **pozitif kontrol delik açıkken yeşildi** — çünkü örnek cümleyi **desenlerden üretiyordu**. Kendi kendini doğrulayan bir pozitif kontrol, **hiçbir şeyin kontrolü değildir**.
2. 🔴 **Yapıcı, BİR TUR ÖNCE kendi bildirdiği kusuru düzeltmesinde tekrarladı.** 3. turda *"bayrakların tasarlandığı bozulma yolu ulaşılamaz"* dedi; 3. turda yazdığı düzeltmenin testi **`Screen`'i hiç çağırmıyordu**, yani üretimin koştuğu yol ölçülmemişti ve kademelendirme **etkisizdi**. **Bir kusur sınıfını adlandırabilmek, ona düşmemeyi sağlamıyor.**
3. **Bir ağ sekiz gündür yarım koşuyordu:** `make audit`'te govulncheck exit 2 verince make duruyor ve **`redline-check.sh` hiç koşmuyordu**. *"Zaten kırmızı"* alışkanlığı, gizlemesi en pahalı şeyi gizliyordu.
4. **Abort olmuş transaction'da `SAVEPOINT` bile reddediliyor (25P02)** — savepoint hatadan **önce** alınmak zorunda; *"sonradan tamir et"* çözümü makul görünüp hiç çalışmazdı.

**Ne kaldı.** **M7-04** (Q02 açık; ve M7-02'nin **(a) limitinin** gerçek kapanışı orada). Backlog **T31–T36** eklendi (**T36 aynı gün kapandı**), **B2 düşürüldü**; **T31 kullanıcının işi** (`go1.26.5 → 1.26.6`).

### 2026-08-13 (9. oturum, devam) — **M7-02 done** · Kayıt sihirbazı · **8 tur, 4 RED, üç farklı üçüncü göz + güvenlik ONAY** · migration 00017 · ADR 0013

**Ne yapıldı.** **Ürünün fonksiyonel boşluğu kapandı: artık herkes kayıt olabiliyor.** `/` → üç adımlı sihirbaz → tenant + mekânlar + ilk sahip **tek transaction'da** → giriş → panel. Commit `9ac3065`.

**Ne öğrenildi — dördü kalıcı:**
1. 🔴 **Bir kabul-edilen-risk notu "süresi doluyor" diyorsa, süresi dolduğunda ölçülecek şey NOTUN TARİF ETTİĞİ ZARAR DEĞİL, KORUMANIN KENDİSİDİR.** `00011` bir CPU amplifikasyonu tarif ediyordu; gerçek kusur `MaxCandidates` kapağının **ödeyen müşteriyi kilitlemesiydi** — beş koşunun üçünde, ve **hiç kimse ölçmemişti**.
2. 🔴 **Bir takası SATIN ALAN cümleyi, takası uygulamadan önce ölç.** Zamanlama kanalını kapatma kararımı *"bütçe zaten sekiz karşılaştırma için boyutlanmıştı"*ya dayandırdım; o cümle hata yolu için doğru, **başarı yolu için yanlıştı**. Onarım **M7-02'den eski** bir deliği de kapattı.
3. **"Bir reddediş, neyin reddettiğini bilmediğin sürece hiçbir şey kanıtlamaz."** Yapıcının tripwire'ı rastgele bir tenant id'sine kapsamlanmıştı, INSERT **yabancı anahtar** tarafından reddediliyordu — mutasyon yeşil döndü. `SQLSTATE 42501` şart koşulunca kırmızı.
4. **Dört kapatmanın aynı şekilde başarısız olması, hepsinin AYNI mekanizmayı hedeflediğinin kanıtıydı** — ve genel ifade (*"doyurulabilir her sınır sinyali yeniden üretir"*) bir denetçi **beşinci** kapatmayı bulunca **geri çekildi**.

**Ve bir kalıp:** *"sağlamadığı garantiyi beyan eden cümle"* bu görevde **dört kez** çıktı — biri **üçüncüsü için inşa edilen düzeltmenin içinde**. Kök neden çalışması (`signupClaims` + dokuz ekranı tarayan ağ) M7-01'den devralındı ve burada da **yazılı sınırı içinde yenilebilir** çıktı.

### 2026-08-13 (9. oturum, devam) — **M7-01 done** · Landing sayfası · **5 tur, 1 RED, iki farklı üçüncü göz + güvenlik ONAY · ADR 0012**

**Ne yapıldı.** `/` artık yaşıyor — ürünün **kimliksiz ulaşılabilen ilk yüzeyi**: hero · üç adım · dört kanıt (**iki sınırıyla**) · karşılaştırma tablosu · fiyat · FAQ · dört yasal iskelet. Commit `3516f59`, **ADR 0012**.

**Ne öğrenildi — üçü kalıcı:**
1. 🔴 **Bir ağ, yakalayamadığını söylemediği sürece olduğundan güçlü görünür.** RED'in sebebi dikkatsizlik değil **ağ yokluğuydu**: fiyat/bedava ay/çerez/slogan pinliyken iddia bloğu **hiçbir testin kapsamında değildi**. Ve düzeltmenin **kendi sınırı** ölçüldü — denetçi geçen bir çapa + yalan bir cümle enjekte etti, paket **yeşil** kaldı → sınır **testin içine** yazıldı.
2. 🔴 **Bir muafiyet istemeden önce, metnin kendisinin yeniden yazılıp yazılamayacağını ölç.** R1'e eklenen iki ifadenin **biri gereksizdi**: `activate.templ:76` aynı sözü **eski süzgeçten geçen** bir biçimde söylüyordu (`no fingerprints` aynı satırda). Metin o kalıba getirildi, ifade kaldırıldı — ve kamuya açık sayfa §4.1 sözünü artık **çalışanın okuduğu ekranla aynı kelimelerle** veriyor.
3. **Bir muafiyet görünmez olamaz** (R5'in ilkesi R1'e taşındı). Güvenlik merceği muafiyetin ilk hâlini **kırdı**; üç koşulla daraltıldı ve **her koşuda `[R1 · WARN]`** basılıyor. Orkestratör sondayı **kendi** tekrarladı: artık exit 1.

**Ve ters bir tuzak:** `documentHead` **her sayfaya** `noindex` gömüyordu — yani pazarlama sayfası kimsenin bulamayacağı bir sayfa olurdu.

**Kullanıcıdan bekleniyor:** dört yasal sayfanın metni (şirket künyesi · veri sorumlusu · saklama süreleri · sözleşme şartları). İskelet hazır, her sayfa **neyi beklediğini** yazıyor. Landing'in geri kalanı bunlara **bağlı değil**.

### 2026-08-13 (9. oturum, devam) — **🎉 M6 TAMAM** · M6-12 FAZ B done · **3 tur, 0 RED, üçüncü göz 1. turda ONAY + güvenlik ONAY**

**Ne yapıldı.** `/admin/billing` (sekizinci bölüm) + CSV + founding uyarısı + iki POST. Bölüm **owner-only**, iki yeni audit eylemi, CSV kaçışı `reportscsv.go`'dan **tamamen** devralındı. Commit `5b2736a`. **M6 kapandı: 12/12.**

**Ne öğrenildi — üçü kalıcı:**
1. 🔴 **Bir emsal ancak MEKANİZMASI da taşındığında emsaldir.** Brief'im *"rol kapısını policy motoruna bağla — M6-09 B'nin yolu"* diyordu. Yapıcı üçünü de ölçüp çürüttü: sözlük kapalı · **DB'de saklı** baseline owner'ı `deny sid=default` ile **kilitliyor** · ve guardrail olmadığı için izin **tenant tarafından ezilebilirdi**. *"M6-09 böyle yaptı"* bir gerekçe değil, bir **hipotez**.
2. **Bir kısıtı NEREYE koyduğun, koyup koymadığın kadar önemli.** Sekmeyi gizleme filtresi **navigasyona** kondu, **rota tablosuna** değil — böylece *"linki 404 veren sekme imkânsız"* özelliği korundu ve manager **handler'dan 403** alıyor. Filtreyi rota tablosuna taşıyan mutasyon **dört testi** kırmızı ediyor.
3. **Bir denetçinin "gözlem" diye yazdığı şey bir karar isteyebilir.** Güvenlik merceği *"manager işletmenin borcunu okuyabiliyor, §4 ihlali değil"* dedi ve kararı bıraktı. Faz A ticari şartları uygulama rolüne **iki fiilde de** kapatmışken okumayı açık bırakmak duruşu yarım bırakıyordu → kapatıldı.

**Ve bir dürüstlük notu yapıcıdan:** üç tam süit koşusu kendi düzenlemeleriyle **yarıştı**, üçünü de **attı**; verdiği rakamlar manifest'in ağacın kımıldamadığını doğruladığı temiz koşudan.

### 2026-08-13 (9. oturum) — **M6-12 FAZ A done** · Dondurulmuş fatura satırı · **5 tur, 3 RED, iki farklı üçüncü göz + güvenlik ONAY**

**Ne yapıldı.** Migration **00016**: `tenants.price_per_employee_month` (numeric) + `billing_periods` — §4.3 append-only ailesine katıldı, yetkileri `transactions` ile **birebir**. Kapatılan dönem sayıyı, birim fiyatı, sınırları, zaman dilimini ve planı **donduruyor**; üç kapı çağıranın `if`'inde değil **ifadenin içinde** ve çağıranın **rakam parametresi yok**. `internal/domain/billing` + 4 sorgu + §4.5 ağı. Commit `e085ae6`. **Faz B (ekran/CSV/uyarı/rota) kaldı.**

**Ne öğrenildi — dördü kalıcı:**
1. 🔴 **Ölçülmemiş bir devir cümlesi, migration yorumuna ve plan kartına kadar yayılır.** Ö5'e *"kirlilik seed'de **ve** test fixture'larında"* yazdım — **ölçmeden**. Seed'in damgayı türettiği ölçüldü (**sıfır** çelişen satır), ama o zamana kadar iddia **üç yere** işlenmişti. Devir notundaki her cümle, brief'e girdiği an **kanıt muamelesi görüyor**.
2. **Bir kısıtın maliyeti, kırdığı test sayısı değil, kırdığı testin NE OLDUĞUDUR.** Elemenin en güçlü terimi 201 kırmızı test değildi: CHECK, **kuralı kanıtlayan fikstürü** reddederek kuralı **test edilemez** kılıyordu.
3. **İki ölçümü birlikte koşturmak ucuzunu maskeler.** İki `CHECK` yarısı aynı koşuda ölçülünce kaskad hatalar ucuz yarımı yuttu (6 sanıldı, **12**). Ayrı koşunca denetçiyle birebir tuttu.
4. **Bir yetki daraltmasının doğru sütun listesi ölçülür, çıkarılmaz.** Güvenlik denetçisinin verdiği listede `id` yoktu; onsuz `DEFAULT gen_random_uuid()` RLS bağlamıyla eşleşmez ve **her signup INSERT'i `WITH CHECK`'e takılırdı**. Yapıcı 33 çağrı yerini sayıp yakaladı — ve `created_at`'i de ekledi (kendi kayıt tarihini yazan tenant **bedava aylarını kaydırır**).

**Ve iki desen tekrarladı:** *"kusuru düzeltip **kardeşlerini aramamak"*** — **dördüncü** kez (1. turda düzeltilen oynak sayının kardeşi **aynı dosyada 100 satır aşağıdaydı**); brief'e *"önce kardeş taraması yap, komutunu yaz"* konunca tarama denetçinin **görmediği üçüncüsünü** buldu. Ve **orkestratör kendi tuzağına düştü**: çıplak `go test -race` koşup **396 SKIP** aldım — projenin belgelenmiş tuzağı, env'i yükleyince **2865/0/0**.

### 2026-08-12 (8. oturum, devam) — **M6-11 done** · Anomali raporu · **3 tur, 2 RED, üç farklı üçüncü göz + güvenlik ONAY**

**Ne yapıldı.** `/admin/anomalies`: GPS-only oranı (kişi + lokasyon) · `ctr` boşlukları (kişi + plaket) · gps-conflict bayrağı **ve** kaçının IP'yle de eşleştiği · birlikte tap yapan çiftler · açık kayıtlar · çapraz-lokasyon · kural kırılımı. **8 salt-okuma sorgusu, migration yok.** Commit `f39a341`.

**Ne öğrenildi — dördü kalıcı:**
1. **Kartı ölçmek brief'in en yüksek getirili adımı.** Devir notuna *"kartın bilmediği ikinci bir kaynaksız kriter var"* diye yazdığım ölçüm (`device_info` 25 değer / 7.430 oturum), yapıcının ilk turda doğru kararı vermesini sağladı. **Üçüncüsünü ben de kaçırdım** (`ever_cross` 974/974) — yapıcı buldu.
2. **Bir duvar saati bir sorgunun özelliği değildir.** Aynı karşılaştırma **dört kez** ölçüldü, dördü de ayrıştı (209 → 17 → 103 → 276 ms), çünkü **üç farklı join stratejisi** seçildi ve pencere her `make test` ile büyüdü. Doğru iddia **kesirdi** (%84 kartezyen) ve o **her makinede** durdu.
3. **Bir desen ne göremediğiyle tanımlanır.** `\d{2,3}` fixture'ın kendi koordinatlarından seçilmişti ve **Malta'yı kodluyordu** — 0°–10° arası her derece görünmezdi. Yapıcı bunu kabul edip **adıyla yazdı**.
4. **"COUNTED" bir etiket değil, bir iddiadır.** Sayılmış-limit listesi *"COUNTED RATHER THAN GLOSSED"* başlığını taşıyordu ve denetçi onu **tek oturumda iki yoldan** yendi. Bir listeyi *sayılmış* ilan etmeden önce **yenmeye çalış**.

**Ve üç kez tekrarlayan bir yapıcı deseni kayda geçti:** *"bir denetçinin adını koyduğu kusuru düzeltip **kardeşlerini aramamak**"* — aynı sınıf `Answerable:0` fixture'larında, `Records is a denominator` cümlesinde ve `report.go` atıflarında tekrarladı.

**Ne kaldı.** **M6-12** (çalışan sayımı + fatura taslağı) — M6'nın son görevi, ve **dondurulmuş sayım tablosu** muhtemelen bir migration ister. Backlog **T23–T25** eklendi.

### 2026-08-12 (8. oturum, devam) — **M6-09 FAZ B done · M6-09 KAPANDI · MIGRATION 00015** · **11 tur, 10 RED — projenin en uzun görevi**

**Ne yapıldı.** `/admin/policies` yazma tarafı: baseline aç/kapa · sevk edilmiş bir kuralı **kendi venue'larına kopyalama** · yeniden adlandırma · bağlama/çözme · **düzenleme = yeni sürüm** · her değişiklik ve **her motor reddi** `audit_log`'a, **aynı transaction'da**, reddin **hangi kapı aşaması**ndan geldiğiyle. **`policy:edit` ürün tarihinde ilk kez gerçekten zorlandı.** Migration **00015**. Commit `2cf55f2`.

**Ne öğrenildi — beşi kalıcı:**
1. **Bir kayıp-önleme talimatı, kilidin NEREDE olacağını söylemezse yeni bir yarış açar.** Orkestratör *"aynı adı ikinci kez yazmayı reddet"* dedi; sonuç bir **oku-sonra-yaz** oldu ve **eşzamanlı bağlantı başına bir geri alınamaz satır** üretti. §4.4'ün *"tek ifadede"* dersi `ctr`'ye özel değil — **DELETE verilmeyen her tabloya** ait.
2. **Bir düzeltme, başka bir yerdeki ölçümü sessizce geçersiz kılabilir.** 7. turun **doğru** fixture düzeltmesi (dokuz belgeyi de seed etmek), kartın *"seçenek (a) ölçümle elendi"* dediği **falsifier'ı öldürdü** — assertion boşaldı, kimse fark etmedi. **Yeni sınıf.**
3. **`make test` ≠ `make check`.** Sekiz tur boyunca *"exit 2 yalnız son `git diff`'ten"* diye raporlandı; gerçek sebep **ilk adımdı** (`templ fmt` kaynağı değiştiriyordu). Ölçümü on saniye: **manifest önce/sonra**. `.templ` düzenledikten sonra **`make gen` yetmez, `make fmt gen`**.
4. **Bir ağı on üç kez yamamak yerine sayacı KALDIR.** Bölge `<div>` sayarak bulunuyordu; her yazım (`<!--`, `<!x`, `<!DOCTYPE`, `<?x>`, `<![CDATA[`) sayacı yeniyordu. Çözüm bileşeni **izole render** edip bölgeyi **onun baytları** yapmak oldu — faz A'nın tekniği bir kat aşağıda, ve sınıf **kapandı**.
5. **Test double'ı sınandığı özellikte üretimden ayrışabilir.** İki vaka: biri hep-ya-hiç kuralını uygulamıyordu, öbürü yetki alanını **hiç okumuyordu** → apply adımının red kolu **ölü koddu**. Düzeltme *"bir assertion ekle"* değil, **sözleşmeye eşitle**.

**Ne kaldı.** **M6-11** (anomali raporu) — kabul kriterlerinden birinin **bugün kaynağı yok** ve karar sahibine bırakıldı (kaynak üret ya da sinyali düşür). Backlog **T22** (`make db-reset` kırık). M6-09'un **karşılanmayan dört kriteri** kartta sayılı.

### 2026-08-12 (8. oturum, devam) — **M6-09 FAZ A done** · Policy ekranı okuma tarafı · **8 tur, 4 RED — projenin en çok RED alan görevi**

**Ne yapıldı.** `6738687` — `/admin/policies` okuma tarafı: üç katman, sürüm geçmişi, guardrail sırası, yetkilendirme bölümü. **Migration yok.** Yazma tarafı **Faz B**. Beş denetçi (üç genel üçüncü göz + `tappa-security-auditor` + kapanış).

**Ne öğrenildi — dördü de bu göreve özgü.**

1. **🔴 BİR EKRANIN EN TEHLİKELİ HATASI, YÜRÜRLÜKTE OLMAYAN BİR KURALI YÜRÜRLÜKTEYMİŞ GİBİ GÖSTERMEKTİR — VE KUSUR YAYILIM ALANINDAYDI.** Dokuz baseline belgesinden **biri** okunamazsa motor guardrail'lere düşüyor; ekran diğer **sekizini `In force`** ve onlardan türeyen izinleri **`Granted to`** diye basıyordu. ⚠️ **Yapıcının kendi yorumu bunu ADIYLA koymuştu** (*"ADR 0004 buna var olan en tehlikeli hata diyor"*) — kusuru **o kural için** çözmüş, **yayılımı için** çözmemişti. **Ders: bir kusuru adlandıran yorum, kusurun KAPSAMINI da adlandırmalı — "bu satır için çözdüm" ile "bu SINIF için çözdüm" ayrı iddialardır.**
2. **🔴 BİR AĞIN TARADIĞI BÖLGE, TARADIĞINI SÖYLEDİĞİ BÖLGE DEĞİLDİR — YEDİ KEZ.** Deny-list dardı · **düzeltmenin kendisi** ağın göremediği yere düştü · dört koşullu bloğu **hiçbir fixture sürmüyordu** (biri üretimde **her sayfada**) · kabuğun **kendi token sözlüğüyle** POST kontrolü kuruldu · **ödünç tanık** · `switch`/düz-Go görünmezliği · ve **öznitelik değerindeki ham `>`** tarayıcıyı körleştirdi. **Yamalar hep yeni bir kapı açtı; kapatanlar ÜÇ TÜRETİM oldu** (fixture kümesi `.templ`'den · token listesi yerine **bayt-tam eşitlik** · tarayıcı okuyamadığını **reddediyor**). **Ders: bir ağ üçüncü kez yenildiğinde yamayı bırak — kapsamı ÜRETEN kaynağa bağla; ve bağlayamadığını İKİNCİ DURMA KURALIYLA say.**
3. **⚠️ *"SAĞLAMADIĞI GARANTİYİ BEYAN ETMEK"* SINIFI TEK GÖREVDE DÖRT KEZ.** Sonuncusu: bir başlık *"her koşullu dalı **TÜRETİR**"* diyordu ve **iki satır altta** *"HER DALI TÜRETMİYOR, VE **BAŞLIK ÖYLE DİYORDU**"* yazıyordu — **başlık değişmemişti**, yani düzeltme metni **yapılmamış bir değişikliği** bildiriyordu. Ve yapıcı kendi limit cümlesini **iki kez olduğundan küçük** yazdı (`switch` *"üç şablonda"* → gerçek **18 kol / 9 şablon**), ikincisinde **türeterek** kapattı. **Ders: bir limit cümlesi de bir iddiadır ve ölçülür; ve bir düzeltme metni yazmadan önce düzeltmenin YAPILDIĞINI grep'le.**
4. **✅ VE İYİ HABER: ÜRÜN YEDİ DENETİMDE DE TEMİZ ÇIKTI.** Sevk edilen şablonda kontrol yok, view'da alan yok, okuma **hiçbir satır yazmıyor** (baseline'sız tenant **0/0/0 → 0/0/0**), guardrail **taklit edilemiyor** (`sys:`/`base:` sid + `ignore`/`redirect` **dördü de reddediliyor**). **Kırılan hep KORUMAYDI, sızıntı değil** — ve bu ayrımı her turda yazmak, dört RED'i panik değil disiplin yaptı.

**Orkestratör hatası (bir tane).** Devir notumda *"M3-06: `tap:*` → `review`, diğer her eylem → `deny`"* yazmıştım — gerçek: `reviewDefaultActions` **yalnız `ActionTapRecord`**, yani **`tap:approve` `deny`'a düşüyor**, ve motorun kendi yorumu bunu *"the ADR's loose shorthand"* diye adlandırıyor. **Kartın *"`tap:approve` tuzağı"* diye uyardığı okuma hatasını devir notunda ben tekrarlamışım.** ⚠️ Yapıcı **tuzağa düşmedi** — ayrımı isimden değil **motora sorarak** türetti, ve güvenlik merceği bunun boş Set ile gerçek Set'te **aynı** cevabı verdiğini ölçtü. Zarar oluşmadı; not düzeltildi.

**Ne kaldı.** **M6-09 FAZ B — yazma tarafı.** On yükümlülük miras (yukarıda "ŞU AN"), ve içlerinden biri **üç görev üst üste ertelendi**: `policy:edit`'in gerçekten zorlanması. ⚠️ B bir **form** ekleyecek — o form **bölümün içinde** olacak ve ağ onu **görecek**; guardrail bölümüne **hiçbir kontrol giremez**.

### 2026-08-10 (8. oturum, devam) — **M6-08 done** · Manuel kayıt · **MIGRATION 00014** · **5 tur, 1 RED**

**Ne yapıldı.** `03a95fa` — ürünün `transactions`'a **ikinci yazıcısı** (`internal/domain/manual/`), Q18'in kapanış ucu, ve **migration 00014** (`verdict IN ('ok','flag') OR type IS NULL`, VALIDATED). **ADR 0011.** Üç denetçi ONAY + bir RED.

**Ne öğrenildi — dördü de bu göreve özgü.**

1. **🔴 BİR GÖREVİN EN DEĞERLİ ÇIKTISI, YAPMADIĞI ŞEYİN ÖLÇÜMÜ OLABİLİR.** *"Düzeltme = yeni satır"* (§4.3) kriteri **yapısal olarak doğru** ama **yarısı çalışmıyor**: eşleme motoru en geç `in` + en erken `out` aldığı için eklenen satır **yalnızca kısaltır**, ve bir `in` düzeltmesi **kapatılamaz** bir açık kayıt bırakır. Yapıcı bunu **kendi kartını yanlışlayarak** buldu, davranışı **değiştirmedi**, ölçtü, **müdürün okuduğu onay ekranına** yazdı ve **ADR 0011**'e kaydetti. **Ders: bir kabul kriterinin YAPISAL yarısı tutarken ANLAMSAL yarısı tutmayabilir — kriteri sağladığını iddia etmeden önce SONUCUNU ölç.**
2. **🔴 BİR YORUMUN DAYANDIĞI ŞEMA KISITI OLMAYABİLİR — VE İKİNCİ YAZICI GELDİĞİNDE BEDELİ ARTAR.** Şemada *"ok ⟹ yön"* vardı, **tersi yoktu**. Üç denetçi de rollback'li sondayla `verdict='reject', type='in'`'i **kabul ettirdi**. 00014 bunu kapattı. ⚠️ **Ama orkestratörün uyarısı da ölçümle çürüdü**: o satır **rapora saat olarak girmezdi**, çünkü **M6-07 A'nın fail-safe'i onu zaten karantinaya alıyordu**. **Ders: bir riski hem YAZ hem ÖLÇ — iki tur önce onayladığın bir savunma, bugünkü tehdidin cevabı olabilir.**
3. **⚠️ DÜZELTME TURU, KENDİ DEĞİŞİKLİKLERİNİN TERSİNİ SÖYLEYEN METİN BIRAKABİLİR — TEK RED BUYDU.** 00014 sevk edildikten sonra **iki dosya hâlâ** *"şema bunu kabul eder"* diyordu (biri **var olmayan bir teste** atıf vererek), bir üçüncüsü *"her sonuç 303 döner"* diyordu (**beş dal 200**), ve migration'ın kendi gerekçesi **sondayla yanlışlandı**. **Ders: bir düzeltme turu bittiğinde, DEĞİŞTİRDİĞİN DAVRANIŞI TARİF EDEN HER CÜMLEYİ yeniden oku — kendi turun dahil.** (M6-06 B'de aynı sınıf: bir kısıt eklemek `Down`'ın sayısını **ve** reçetesini birden bozmuştu.)
4. **🔴 `make test`'İN DETERMİNİSTİK OLMADIĞI İKİNCİ KEZ ÇIKTI.** `TestPlaqueJourneyDB_...` kendi tenant'ını zehirliyordu — **P(fail) ≈ %28,8 ve her koşuda artıyor**, yani **bu kilometre taşındaki her *"suite yeşil"* iddiası olasılıksaldı**. `vat_number` çakışmasından sonra bu sınıfın ikinci vakası. **Ders: paylaşılan bir dev DB'ye YAZAN her test, yazdığını GERİ ALMALI ya da kendi tenant'ında koşmalı — ve bir denetçiye "suite yeşil" dedirtmeden önce EN AZ İKİ koşum iste.**

**Orkestratör hataları (bu turda beş + bir talimat).** `sun_valid=false` (doğrusu **NULL**) · **`RecordTransaction` diye bir sorgu yok** (adı `InsertTransaction`; M6-07'nin güvenlik denetçisinden **doğrulamadan** taşıdım) · zemin `2493/16 paket` → **2597/17** · migration `00013` → **00014** · *"geriye tarihli manuel giriş doğru geç kalma üretir"* — **doğru sonuç, yanlış mekanizma** (manuel kayıt `tap.Decide`'a **hiç uğramıyor**). ⚠️ Ve bir **düzeltme talimatım yanlıştı**: yapıcıya *"kuşak ağı 27/69 → 27/70"* dedim; **69 devir anında doğruydu**, 70 ancak M6-08 kendi sorgusunu ekledikten sonra oldu. **Ders: başka bir ajanın raporundan aldığın bir ADI veya SAYIYI, kendi brief'ine koymadan önce doğrula.**

**Ne kaldı.** **M6-09 — Policy yönetim ekranı.** ⚠️ Bu, panelin policy motorunu çağıran **ilk ekranı** olacak ve **iki görev üst üste** onu bilinçli olarak çağırmadı; `forTenant`'ın **baseline'ı materialise etmesi** çözülmesi gereken mimari sorun. İki bulgu **backlog'a**: türetilmiş §4.5 ağının **`INSERT … VALUES` kör noktası** (7 sorgu) ve `problemPanelUnavailable`'ın **çevrilmemiş 13+5 çağrı yeri**.

### 2026-08-10 (8. oturum, devam) — **M6-07 FAZ B done · M6-07 KAPANDI** · CSV export · **4 tur, 0 RED**

**Ne yapıldı.** `3930a52` — `GET /admin/reports.csv`, ekranın **aynı `ledger.Report`**'undan. **Yeni migration YOK, yeni sorgu YOK.** A'nın devrettiği **sekiz yükümlülüğün sekizi de** kapatıldı. **Üç denetçi ONAY.** M6-07 toplamı: **8 tur, 0 RED, altı denetçi.**

**Ne öğrenildi — üçü de bu faza özgü.**

1. **🔴 BİR SINIFIN YARISINI DEVRALMAK — VE DEVREDENİN ADINI YAZMIŞ OLMASI.** Kaçış ağı `unicode.IsControl` kullanıyordu; Go'da o **yalnız Cc**, ve **13 Cf runeu çıplak geçiyordu**. Güvenlik merceği öldürücü kanıtı buldu: **`internal/session/manager.go:512` bu sınıfı ZATEN çözmüş** (`case unicode.Is(unicode.Cf, r)`) **ve o dosyanın doc yorumu formül nötralleştirmeyi ADIYLA M6-07'ye devretmiş.** Devralan taraf sınıfın yarısını almış. **Ders: bir görev sana ADIYLA bir iş devrediyorsa, DEVREDENİN O İŞİ NASIL ÇÖZDÜĞÜNÜ OKU — çözüm zaten repoda olabilir.**
2. **🔴 BİR SINIRIN İLKELİ OLUP OLMADIĞI, ONU DIŞLAYAN GEREKÇEYLE SINANIR.** `Mc` dışarıda bırakılmıştı, gerekçe *"boşluk kaplar → saklanma yeri değil"*. Doğrulayıcı göz **`U+3164` HANGUL FILLER**'ı gösterdi: kategori **`Lo`**, ve **sıfır genişlikte** render oluyor — yani o gerekçeye göre **içeride olmalıydı**. ***"Sınır ilkeli değil, aramanın durduğu yer."*** Çözüm listeyi uzatmak değil, ilkeyi **Go'nun shipped ettiği `Other_Default_Ignorable_Code_Point`** özelliğine bağlamak oldu. **Ders: elle çizilmiş bir sınıf listesi, onu üreten ilkeyi bulana kadar hep bir üye eksiktir.**
3. **⚠️ İMZA DERSİ BU KEZ BİR DENETÇİDE ÇIKTI.** İki denetçi `ReportEventCap` mesafesinde çelişti: biri **ham** `type IN ('in','out')` saydı (*"eşiğe 869 satır"*), diğeri sorgunun **gerçekten okuduğu pencereyi** (`NOT practice` + 18 sa `StaleOpenIn` kuyruğu). Yapıcı dört varyantı çıkardı, üçüncü göz **dördünü de bağımsız SQL'le** üretti: **%96 · %111 · %54 · %63**. **Farkın tamamı `NOT t.practice`.** *Ölçüm doğru, popülasyon uydurma* — ve bu kez uyduran **denetçiydi**. **Ders: bir denetçinin sayısı da popülasyonuyla birlikte okunur.**

**Orkestratör hataları (ikisi de benim).** (a) `state.md` devir notumdaki `2416 PASS` ve `1,64×` kapanışta yanlışlandı — ikincisi **yanlış değil bayattı**, doğru popülasyondan geliyordu. (b) 🔴 **`state.md`'yi bir python betiğiyle düzenlerken dosyayı SIFIR BAYTA düşürdüm**: betik dosyayı yazmak için açtı (**truncate**), sonra bir surrogate kaçışında patladı. Commit'li sürümden geri alınıp yeniden uygulandı, **veri kaybı yok**. **Ders: `state.md` gibi tek-kaynak dosyalarda in-place python yazımı yapma — atomik `Edit` kullan, ya da önce geçici dosyaya yazıp `mv` et.**

**Ne kaldı.** **M6-08 — Manuel kayıt girişi.** ⚠️ Bu, ürünün `transactions`'a **ikinci yazıcısı** ve M6-07'nin ölçtüğü bir bariyeri **taşıyıcı** hâle getiriyor (yukarıda "ŞU AN", madde 1). İki bulgu **backlog'a**: audit yazımı düşerse toplu çıkışın **izsiz** kalabilmesi, ve çapraz-origin gezinmenin **append-only bir tabloya kimsenin silemeyeceği** bir satır yazdırabilmesi.

### 2026-08-10 (8. oturum) — **M6-07 FAZ A done** · Reports · **4 tur, 0 RED — projenin İLK sıfır-RED görevi**

**Ne yapıldı.** `671289b` — panelin Reports sekmesi: kişi başına haftalık saat + günlük kırılım, lokasyon
kırılımı, geç kalma **çalışanın KENDİ vardiyasına** göre, ve **toplama girmeyen** açık girişler kendi
bölümünde. Saat motoru `internal/domain/ledger/report.go`, iki yeni sorgu. **Migration yok.** CSV **Faz B**.
**Üç denetçi ONAY** (genel · `tappa-security-auditor` · düzeltme turunu doğrulayan yeni göz).

**Ne öğrenildi — üçü de bu oturuma özgü.**

1. **🔴 SIFIR RED, AMA ON İKİ BLOKLAMAYAN BULGU — VE ONU İMZA SINIFIYDI.** *Bir ağın yakaladığı ile
   yakaladığını SÖYLEDİĞİ ayrı iki iddiadır.* Bu oturumda **üründe tek sızıntı yoktu**; yanlış olan
   **yazılı kapsamdı**, on kez. Ve sınıf **kendi düzeltmesinde tekrarladı**: §4.7 ağı dokuz addan alt
   dizeye çevrildikten **sonra** ikinci denetçi **`RemoteAddr`**'ı geçirdi — `address` token'ı vardı,
   **`addr` yoktu**, ve `RemoteAddr` Go'nun `source_ip` için **kendi alan adı**. **Ders: bir ağı
   genişletmek, genişletilmiş hâlini YENMEYİ atlamak için gerekçe değildir.**
2. **🔴 BİR YORUMUN DAYANDIĞI ŞEMA KISITI VAR OLMAYABİLİR — VE FAIL-OPEN TAM ORADA SAKLANIR.**
   `endpointState`'in `default`'u bilinmeyen her verdict'i **ödenebilir** sayıyordu; savunması
   *"migration 0005'in CHECK'i yalnız zincire katılanlara yön verir"*di. Şemada öyle bir CHECK **yok** —
   tek kısıt **tek yönlü**. Rollback'li bir sonda `verdict='reject', type='in'` satırını **kabul ettirdi**.
   Bugün ulaşan yol yok; ama **bariyer bir kod değişmezi**, ve M6-08 ikinci yazıcıyı ekliyor. **Ders:
   bir yorum bir DB kısıtına atıf yapıyorsa, kısıtı `pg_constraint`'ten OKU — atfın kendisi kanıt değil.**
3. **⚠️ ÜÇÜNCÜ KEZ AYNI `EXPLAIN`, ÜÇÜNCÜ KEZ FARKLI İNDEKS** — ve üçüncüsünü **bağımsız bir okuyucu
   AYNI GÜN** çürüttü. **Ders: bir plan adı ölçüm değil, o anın istatistiklerinin fonksiyonudur;
   yazılacak şey plan ŞEKLİ + büyüklükler + tarihtir.** İndeks adı bloktan tamamen silindi.

**Orkestratör hatası (denetçi buldu, benim).** `state.md`'nin M6-07 devir notundaki **dört cümle**
yanlışlandı: `2335 PASS` → **2416** · kuşak ağı `27/67` → **27/69** · **T17 yanlış tabloyu** gösteriyordu
(A `audit_log`'a **hiç dokunmadı**; ama uyarının *şekli* tuttu) · ve M6-06 kalıpları maddesi A'ya
**uygulanamazdı** (A hiçbir şey yazmıyor). Devir notu yenilendi, eskisi **tarihsel** olarak bırakıldı.
**Alt ajanlar `state.md`'ye dokunamadığı için bunu yalnız denetçi yakalayabilir** — brief'e koymak şart.

**Ne kaldı.** **M6-07 FAZ B — CSV export.** Sekiz yükümlülük miras (yukarıda "ŞU AN"): aritmetiği
**yeniden yazmama** · `= + - @` kaçışı · §4.7 · `audit_log` (B'nin **tek yazışı**) · `report:export`
(panelde `Evaluate` çağıran handler **yok**) · UTC+yerel/ISO 8601 · `ReportEventCap` kesilmesi
(**1,64×** en yoğun hafta, yani ulaşılabilir) · ve ekranın satır tavanlarının CSV'de **olmaması**.

### 2026-08-10 (7. oturum, devam) — **M6-06 B PANEL done · M6-06 KAPANDI** · **8 tur, 4 RED**

**Ne yapıldı.** `4ec5e85`. Plaket listesi · **replace + mount + un-mount** · `audit_log`'dan *"kim ne yaptı"* ·
*encoded/pending*. **Yeni migration yok**, iki yeni sorgu. **M6-06 üç parçada 31 tur sürdü ve kapandı.**

**🔴 BU TURUN EN İYİ DERSİ BİR MUTASYONUN KIRMIZI VERMEMESİNDEN ÇIKTI.** `Unmount`'un atomik önkoşulunu
oku-sonra-yaz ile değiştirmek **n=10 ve n=30'da yeşil** kaldı — çünkü `Unmount` satırı **önce okuyor**
(ize yazacağı duvar için, ki yazma onu yok ediyor), yani READ COMMITTED altında geç goroutine **o okumada**
reddediliyor ve UPDATE'e hiç ulaşmıyor. Yapıcının cümlesi: ***"o test SONUCU kanıtlıyor, MEKANİZMAYI
değil."*** Mekanizmayı pinleyen yeri buldu: **`Mount`'un ön-okuması yok**, her goroutine UPDATE'e ulaşıyor,
aynı mutasyon orada **deterministik KIRMIZI**.

**🔴 "ŞEMA TERSİNİR" İLE "MÜDÜR GERİ ALABİLİR" AYRI CÜMLELER — ve ikincisi eksikken kapı ÇÖZÜM DEĞİL.**
Mount geri alınamıyordu (yanlış duvara mount → panelden **hiçbir kurtarma**, yedek yoksa **hiçbir kontrol**,
tek çıkış çalışan bir plaketi **kalıcı emekliye ayıran** replace) ve tersini söyleyen **iki cümle *"measured
rather than assumed"* etiketi taşıyordu**. **Orkestratör kararı: kapı değil GERİ ALMA YOLU** — kapılamak
yanlış mount'u **engellemez**, yalnız bir tıklama ekler; zarar **geri alınamazlıkta**. `UnmountTagFromWall`
sevk edildi, **mount kapısız kaldı ve cümle DOĞRU oldu**.

**⚠️ İKİ ALT SINIF KAPANDI, İKİSİ DE TÜRETİMLE.** *Elle bakılan bir N listesi, N büyüyünce delik açar*
(**yedi kez**) → eylem sözlüğü `go/ast` ile domain'den, yazma rotaları **gerçek router'dan** `chi.Walk` ile
türetiliyor; ikincisi **Faz A'nın dört rotasını da** kapsadı, ki onlar bu özellikle **hiç kapsanmamıştı**.
*Sayı-bayatlaması* (**on dört kez**) → **mekanik kural** kondu (*kodun sahip olduğu bir kümeyi tarif eden
çıplak tamsayı yorumlarda yer almaz*) ve beş dizin süpürüldü: **169 aday → 17 kod-sahipli yer, 11'i sayıyı
SİLEREK**.

**⚠️ VE İKİ RAPOR İDDİASI ÖLÇÜMLE YANLIŞLANDI** (*"iki paragrafı yeniden yazdım"* — yazılmamıştı;
*"o sayı artık hiç yazılmıyor"* — yazılıyordu **ve 9 kat yanlıştı**). İkisi de **denetçi tarafından**
yakalandı, ikisini de yapıcı sahiplendi. Ritmin cümlesi: ***"the rhythm assumes the builder is fallible
rather than trusting the report."***

**Ne kaldı.** **M6-07** (Reports ve CSV export). Bağımlılıkları done. ⚠️ Kart **Q18'i** taşıyor ve çok
ayrıntılı; ve **T17** (`audit_log`'da `(tenant_id, target)` indeksi yok) rapor sorgularının şeklini
etkileyebilir.

### 2026-08-09 (7. oturum, devam) — **M6-06 B VERİ KATMANI done** · migration 00013 · **7 tur, 3 RED**

**Ne yapıldı.** `ba671b0`. `tags` sertleştirmesi (T8 + T9) + **envanter modeli** + T11 indeksi + Faz A'nın
devrettiği ad CHECK'leri. **Uygulama katmanı ayrı tur**, **on yükümlülük** miras alıyor.

**🔴 BU TURUN DERSİ: BİR BORÇ MADDESİNİN ÖNERDİĞİ ÇÖZÜM DE, ÖNCÜLÜ KADAR ÖLÇÜLMELİDİR.** Üç devraldığımız
cümle çürüdü. **T8'in öncülü** (*"mevcut satırlar zaten büyük harf"*) yanlıştı ve düzeltmesi **§4.3'e
çarpıyordu**: 18.010 küçük harfli satırın 12.437'si **append-only `transactions`'ın 24.874 satırından**
referanslı, yani *"büyük harfe çevir"* = tetikleyiciyi superuser'la kapatıp **delil satırlarını yeniden
yazmak**. **T9'un önerdiği trigger koşulu** (`> OLD`) `last_ctr`'a dokunmayan bir **retire'ı reddediyordu**,
ve **sütun listesi eksikti** — *aynı günün iki kullanıcı kararı birbirini uygulanamaz kılıyordu.*

**🔴 BİR GUARDRAIL FAIL-OPEN'DI.** `sys:tag-not-active` bir **denylist**'ti ve migration'ın eklediği
dördüncü durum **altından geçiyordu**. Güvenlik merceği zinciri sürdü: NFC kapalı (**yanlış gerekçeyle**),
**QR açıktı**. Orkestratör kararı: **şimdi kapat, devretme** — eşdeğerlik **migration'ın CHECK kümesinden
türetilerek** ölçüldü. Kazanç: **beşinci durum, kimse hatırlamasa da eklendiği gün reddedilir.**
⚠️ Migration kendi riskini **fazla** yazmıştı (*"can end as ok"* — `ok` erişilemez, en kötü `flag`);
**riski abartmak da bir ölçüm hatasıdır.**

**⚠️ BİR DÜZELTME, AYNI DOSYADAKİ BAŞKA BİR CÜMLEYİ GEÇERSİZ KILDI.** 4. turda eklenen bir CHECK, `Down`
bloğunun kısıt **sayısını** ve belgelenen **kurtarma reçetesini** birden bozdu — reçete operatöre şemanın
**kendisinin yasakladığı** bir yolu tarif ediyordu.

**⚠️ VE `make test` GÜVENİLMEZDİ.** `vat_number` sekiz hex haneden üretiliyordu; çakışma **koşum başına
%1,13 (~89 koşumda bir)** ve artıyordu. 19 dosyada düzeltildi → **9,2e-30**. Bu, oturum boyunca yapılan
her *"PASS sayısı birebir tuttu"* doğrulamasının **zeminiydi**.

**Ne kaldı.** **M6-06 B uygulama katmanı** — plaket listesi · replace tag · tag geçmişi · *encoded/pending*.
⚠️ **Yeni migration yok**: 00013 B'nin yuvasıydı ve harcandı. En ağır devir: **QR yolu** ve
`preview.go`'nun **yanlış gerekçe metni**.

### 2026-08-09 (7. oturum) — **M6-06 A fazı done** · Locations & Departments · **16 tur, 5 RED — projenin en uzun görevi**

**Ne yapıldı.** `d010c1f`. Görev **denetim merceğine göre** A/B'ye bölündü (dördüncü kez): A = lokasyon/departman
(§4.5 · §4.6 · yetkilendirme), B = plaketler (§4.7 AES · §4.4 replace tag) **artı migration 00013'te T8+T9**.
A fazı: lokasyon CRUD · departman yönetimi · **referanssıza silme** · **C′ audit-destekli silme bildirimi**.
**Migration yok** — ve bu **varsayılmadı**: `tappa_app`'in iki tabloda da DELETE'e sahip olduğu ve RLS
politikalarının `FOR ALL` (`polcmd='*'`) olduğu ayrı ayrı ölçüldü.

**Beş üçüncü göz, dördü RED, artı `tappa-security-auditor` ONAY.** Altı kusur kapandı; dördü ürünü doğrudan
vuruyordu (NaN enlem → 500 + sekiz alan çöp · tavan ötesi satırlar **hiçbir rotadan** düzenlenemiyor · reddedilen
departman düzenlemesi müdüre **DB bozuk** diyor · `?done=venue-deleted` **olmamış bir silmeyi** ilan ediyor).

**🔴 BU OTURUMUN DERSİ: BİR AĞIN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİA — ON BİR KEZ.** Bloklayanların
çoğu buydu ve **hiçbiri üründe sızıntı değildi**; yanlış olan **yazılı kapsamdı**. En öğretici üçü:
1. **NaN düzeltmesi yanlış satıra atfedilmişti.** Muhafızı eski hâline döndürmek suite'i **yeşil** bırakıyordu,
   çünkü `plainDecimalRE` NaN'ı muhafız görmeden reddediyor. **Kaldırılmasını hiçbir testin fark etmediği bir
   savunma, savunma değildir.** Çözüm: muhafızı **bağımsız** pinleyen bir birim testi.
2. **Bir alt-vaka TAM VAKUMDU ve davranışsal bir test onu ASLA göremezdi** — replacer `,`→`.` yapıyor, `parseClock`
   **iki nokta** istiyor, hiçbir değişim iki nokta üretemez. Vaka **çağrı-grafı ağına** çevrildi… ve o ağ da
   **paket** kapsamı iddia edip **tek dosya** tarıyordu (denetçinin `locationactions.go`'ya koyduğu gerçek sızıntı
   paketi **160,5 sn yeşil** bıraktı) → `go/ast`'a taşındı. ⚠️ Yapıcı yeni ağın **iki kaçışını kendi ölçtü ve
   kendi yazdı** (izinli fonksiyon içinde takma ad · metot değeri) — ilk taslağı *"takma adı yakalar"* diyordu.
3. **Yasaklı-dize taraması ile izinli-anahtar kümesi aynı şey değil.** Ret satırının §4.7 taraması yedi kelimeye
   bakıyordu; **oturum id'si hiçbirine çarpmıyordu**, base64 bir jeton da çarpmazdı.

**🔴 VE BİR ÖLÇÜM İKİ KEZ DOĞRU AMA POPÜLASYONU UYDURMAYDI.** *"Dört `EXISTS` = 8,154 ms"* yeniden üretilemedi
(**171–218 ms**); sebep: `EXISTS`'lerin `OR`'u **kısa devre yapıyor** ve o tenant'ta **9 lokasyonun 9'unda çalışan**
var, yani `transactions` **hiç değerlendirilmiyordu** — sıra değişince **~125 ms**. Düzeltmenin kendisi de yanlıştı
(**tenant filtresiz tam tablo taraması**). Yapıcının cümlesi: ***"Her iki seferde de sayı doğruydu ve popülasyon
uydurmaydı."*** Gerçek değişken **tenant'ın işlem hacmi** — aynı ifade **0,55 ms ile 242 ms** arasında geziyor.
**Kural üç yere yazıldı:** *bir zamanlama, tenant'ı, satır sayısı ve işlem sayısıyla yazılır, ya da hiç yazılmaz.*

**Beş kullanıcı kararı** (A/B bölünmesi · **envanter modeli** — anahtar panelden geçmez · **T8+T9 birlikte 00013'te** ·
**referanssıza Delete** · **ondalık virgül normalleştirmesi** — Malta/Türkiye virgül kullanır ve yapıştırma
belirsizliği **ölçüldü, asılsız** · **C′** · **`owner`-only silme** · ret satırı `audit_log`'a) ve **üç orkestratör
kararı** (ADR **gerekmiyor** — koordinat yazan **1/27**, eşik 2/27 karta yazıldı · **uygulanmış migration'a
dokunulmadı**, 00005'in çelişen yorumu `venue.go`'da **atıfla** uzlaştırıldı · beş sayılmış limit **backlog T11–T14**).

⚠️ **Oturum ortasında kullanıcı otonomi verdi** (*"önerilerin varsa sen otomatik onayla"*) — sorulan yedi sorunun
**yedisinde de** öneri seçilmişti. Sonraki kararlar ölçülüp **uygulandı ve raporlandı**, sorulmadı.

**Yapıcı kendi hatasını DOKUZ kez bildirdi** ve sonuncusu ritmi kurtardı: mutasyon aracının `Makefile.probe`'u
`go test …; echo "EXIT=$?"` ile bitiyordu — `;` yüzünden make **daima 0** dönüyor — ve yanlış paketi sabitliyordu,
yani **iki mutasyon sahte YEŞİL** raporlandı. Kendi çıktısındaki `--- FAIL` verdict'iyle çelişince fark etti, aracı
yeniden yazdı (`$?` yayan, **derlemeyen mutasyona güvenmeyi reddeden**) ve dördünü de yeniden koştu: **hepsi KIRMIZI**.

**Ne kaldı.** **M6-06 B fazı** (plaketler). Devri ağır ve **hepsi karara bağlı**: migration **00013** (T8 + T9,
**T11 aday**) · **envanter modeli** · ve kartın `encoded/pending` kriteri bugün **temsil edilemiyor**
(`aes_key_ref bytea NOT NULL`). ⚠️ `tags`'a **üretim INSERT yolu yok** ve T8'in *bugün sömürülemiyor* koruması
**tam olarak buna** dayanıyordu — **B onu açan görev.**

### 2026-08-08 (6. oturum, devam) — **M6-05 B fazı done · M6-05 KAPANDI** · **6 tur, 4 RED**

**Ne yapıldı.** `77dcb92` + **migration 00012**. Davet/yeniden davet · deaktive · taşıma; üç POST rotası,
her aksiyon audit'iyle **aynı transaction'da**. **M6 5/12.**

**Ne öğrenildi — üç şey, üçü de brief'te.**
1. **🔴 BİR KUSURUN YAŞI İLE ERİŞİLEBİLİRLİĞİ AYRI ŞEYLERDİR.** Kardeş-davet deliği M5-02'den beri
   şemadaydı ve hiçbir test onu görmüyordu, çünkü **hiçbir test iki davet üretip birini harcadıktan sonra
   diğerini denemiyordu**. Onu bulduran şey kod değişikliği değil, **kanalın ilk kez üretim yoluna
   bağlanması** oldu: *"iki kez bas"* bir test senaryosuyken **tek tıklık bir müdür işlemine** dönüştü.
   **Kural: var olan bir mekanizmayı ilk kez bir kullanıcı yüzeyine bağlarken, o mekanizmanın ESKİ
   varsayımlarını yeniden ölç** — kapsam *"yeni kod"* değil, **yeni erişilebilirlik**.
2. **🔴 ŞEKİL MEKANİZMA DEĞİLDİR — üçüncü kez.** Onay kapısında çerez, alan ve sabit zamanlı karşılaştırma
   vardı; **anahtar yoktu**, ve bir denetçi kendi çerezini basıp geçti. Önceki ikisi M6-01 B (`tap.go`'nun
   üç aşamasından biri) ve M6-04 (`sameOriginGate`'in sırası). ⚠️ Çare *"kalıbı say"* — ama **sayının
   kendisi de ölçülmeli**: ikinci denemede *"on parça"* sayıldı, denetçi **on birinciyi** buldu.
3. **🔴 İKİ AĞIN ARASINDAKİ DELİK, ve BİR AĞIN İSTEMCİYİ ÖLÇMESİ.** Tüketen ifadeden yüklem silinince
   uçtan uca test yeşil kaldı çünkü **daha erken bir katman** reddediyordu — iki doğru katman, aralarında
   delik. Ve tek-atımlıklık testi replay'i `browser` yardımcısı üzerinden yapıyordu; o yardımcı **çerez
   silmeyi uyguluyordu**, yani assertion **sunucunun değil test yardımcısının işbirliğini** ölçüyordu.
   **Kural: bir ağın ölçtüğü şeyin ÜRÜNDE mi yoksa HARNESS'TA mı olduğunu sor.**

**İki kullanıcı kararı.** Migration 00012 (kısa TTL **pencereyi daraltır, kapatmaz**) ve onayın sunucuda
**zorlanması** (belgelenmesi değil).

**Ne kaldı.** **M6-06** (Locations & Wall Tags). ⚠️ Devraldığı iki açık borç `tags` üzerinde ve ikisi de
**migration** ister: backlog **T8** (uid'in iki yazımı, aynı AAD) ve **T9** (`tappa_app` `last_ctr`'ı
**geri sarabiliyor**, §4.4).

### 2026-08-07 (6. oturum, devam) — **M6-05 A fazı done** · Employees listesi · **6 tur, 4 RED**

**Ne yapıldı.** `1998e89`. Görev **denetim merceğine göre** A/B'ye bölündü (üçüncü kez; ölçüt kapsam değil,
**farklı saldırılar**). A fazı listeyi verdi: ad · lokasyon/departman · durum · **oturum durumu** · keyset
sayfalama **50**'de · ad/durum filtresi. **Migration yok.** Aksiyonlar B'de ve diff'te **buton bile yok**.

**Ne öğrenildi — üç şey, üçü de brief'te.**
1. **🔴 BİR AĞIN YAKALADIĞI İLE YAKALADIĞINI SÖYLEDİĞİ AYRI İKİ İDDİADIR, VE İKİNCİSİ ÖLÇÜLMEZSE DAHA
   TEHLİKELİDİR.** §4.7'nin tip duvarı **gerçekten çalışıyor** (yeni alan → kırmızı) ama *"bir önek de burada
   kapanır"* **yanlıştı**: var olan bir alanın takma adı altından geçen `left(token_hash,8)` yeni alan
   istemiyor ve **suite 16/16 yeşil** kalıyor. Aynı sınıf üç kez daha (kapsam sayısı kendi komutuyla çelişti ·
   bir gerekçe **var olmayan bir rota tablosu** beyan etti · alt sorgu tarayıcısı 21 saldırının 7'sinde
   yenildi). **Kural: bir ağın cümlesini yazmadan önce ağı YEN; yenemediğini SAY.**
2. **AYNI RED İKİ TUR ÜST ÜSTE GELİYORSA, DÜZELTME YANLIŞ KATMANDADIR.** Kadro boyu **altı kez** kaydı
   (8.718 → 9.138, bir denetimin **içinde iki kez**); iki turda da yanıt *"daha taze bir sayı yaz"* oldu ve
   her taze sayı **tur içinde** bayatladı. Çözüm sayıyı tazelemek değil, **alıntılamayı bırakmaktı** — argüman
   artık **eşitsizlikten** kuruluyor. ⚠️ Ve teli **yanlış alarm verecek kadar** genişletmek reddedildi
   (30 meşru ölçüm): *yanlış alarm veren bir tel, bir sonraki kişinin sileceği teldir.*
3. **GÜVENLİK MERCEĞİ, İKİ GENEL TURUN ÜSTÜNDEN GEÇTİĞİ BİR §4.6 KUSURUNU BULDU — ve kusur kodda değil,
   UYGULANMAYAN BİR VARSAYIMDAYDI.** `maxRosterCursorName`'in yorumu başarısızlık modunu **doğru teşhis
   edip** *"hiçbir bordroda böyle bir isim olmaz"* diyerek duruyordu; şemada CHECK, kodda validator, testte
   vaka **yoktu**. **Kural: bir yorum bir varsayıma yaslanıyorsa, o varsayımı UYGULAYAN şeyi göster.**

**İki kullanıcı kararı.** Sayfa **50** (25'te bütçeyi aşan tek tenant kendi kadrosunu yürüyemiyordu) ve
**kart koda yenildi**: deaktivasyon oturum **iptal etmez**.

**Ne kaldı.** **M6-05 B fazı.** ⚠️ Üç denetçi de ayrı ayrı işaret etti: `admin_users.status='disabled'`
maddesi ON İKİ LİMİT'te **M6-05'e verilmiş** ama kartın A/B bölünmesinde **anılmıyor** — B'nin kartı
yazılırken düşmemeli.

### 2026-08-07 (6. oturum) — **M6-04 done** · FLAGGED onay kuyruğu · **9 tur, 6 RED**

**Ne yapıldı.** `2e7ec64`. Altıncı panel bölümü ve **panelin ilk mutasyon rotası**. Karar
`transaction_reviews` + `audit_log`'a tek transaction'da; `transactions`'a hiç dokunulmuyor. **Migration
yok** — 00005 hazırdı. **M6 4/12.** Ayrıca **ADR 0009** ve bir **kapsam genişlemesi**: `make check` artık
`gen` koşuyor.

**Ne öğrenildi — dört şey, dördü de brief'te.**
1. **🔴 BİR AĞIN KENDİ MERKEZÎ CÜMLESİ, AĞIN KENDİSİ KADAR ÖLÇÜLMELİ.** Bu görevin **dört** ağı, tam olarak
   yapmadıkları şeyi iddia eden bir cümle taşıyordu — ve dördü de **ölçümle** yenildi. En öğreticisi:
   kuşak ağı önce `\bq\.` (alıcının adı), sonra `\.` (üç sözdizimi şekli), sonra AST'ye taşındıktan sonra
   **okuyucusu** (sabitlenmemiş `-- name:` araması) kaçırıldı. Her seferinde *"non-deletable without a red
   test"* cümlesi yerinde duruyordu. **Kural: bir ağ yazarken cümleyi yazmadan ÖNCE üç şekille yen.**
2. **Kopyaladığın kalıbın parçalarını SAY** — ikinci kez. M6-01 B'de orkestratör `tap.go`'nun üç aşamalı
   kalıbının yalnız birini kopyalamıştı; burada aynı hata `sameOriginGate`'te tekrarlandı ve **ölçüm**
   (resolver sayacı: review 1, logout 0) ortaya çıkardı. Yapısal düzeltme yetmez — **sıranın kendisi
   pinlenmeli**, yoksa bir sonraki dokunuşta sessizce geri kayar.
3. **Bir seçeneği "olmaz" diye sunmak, ölçüp göstermekten farklıdır.** İki kullanıcı kararının ikisinde de
   ajan **iki yolu da inşa etti, ölçtü, geri aldı** ve sayılarla önüme koydu. `layout.Panel` kararını süren
   şey bir görüş değil tek bir ölçümdü: **(b)'de tek testi nötralize edip düzenlemeyi yapınca derlendi ve
   paket yeşil kaldı; (a)'da derlenmiyor.**
4. **Sayı-etiketi sınıfı 11. kez ısırdı, ve bu kez *tek ölçümü nokta olarak yazmak* şeklinde.** Yapıcı
   `make gen`'i **3,34 sn** bildirdi (en sıcak tek okuma), gerçek **10–15 sn**. Kendi geri çekti.
   ⚠️ **Benim iki sayım da yanlıştı** — brief'e *"`transaction_reviews` 0 satır"* yazmıştım ama o çıktıyı
   `head` kesmişti, **görmeden yazmışım** (gerçek 9.813); ve *"31.193 flag"* DB geneliydi.

**İki sevk edilmiş kusur sınıfı, ikisi de bu görevin dışına bakıyor.** (1) `make check` **`gen` koşmuyordu**
→ bayat bir `_templ.go` ile commit edilen `.templ`, check'ten **ve CI'dan** geçiyor, ürün eski markup'ı
render ediyordu. Düzeltildi; `ci.yml:88-90`'ın yanlış iddiası da **doğru hâle geldi**. (2) Verilmiş bir
review **geri alınamıyor** ve §4.3'ün telafi yolu bu tabloda yok — ihlal değil, bilinçli, ama yazılı
değildi → **ADR 0009**.

**Ne kaldı.** **M6-05** (Employees). En ağır devri: M6-03'ün *"kim çalışıyor?"* keşif yeteneği bu sekmeye
taşındı — aynı bayt hatasını burada tekrarlama, **sayfala veya arat**. Ve M6-01 B'nin 1. limiti
(`password_hash` yalnız `adminauth.Hash`'ten) burada **süresi doluyor**.

### 2026-08-06 (5. oturum, devam) — **M6-02 + M6-03 done** · panel kabuğu ve Transactions · **10 + 8 tur**

**Ne yapıldı.** M6-02 (`6757537`) kabuğu verdi: `layout.Panel`, `TabBar`, `EmptyState`, üç CSS ailesi, ve
**tek tablodan** `Protect()` içinde mount edilen beş sekme. M6-03 (`37032d0`) Transactions'ı doldurdu:
docket kartları, altı filtre, keyset sayfalama, HTMX. **M6 3/12.**

**Ne öğrenildi — üç şey, üçü de brief'te.**
1. **Kartı ölçmek iki görevde de kendini ödedi.** M6-02'de motifin **M0 iskeletinden** geldiği (M5-06'dan değil)
   ve perforasyon görselinin **hiç var olmadığı** çıktı → üç kriter zaten karşılanmıştı. M6-03'te listeleme
   sorgusunun **hiç olmadığı**, indeksin ise **zaten var olduğu** çıktı. **İkisinde de orkestratörün brief'i
   de yanlıştı ve yapıcı ölçümle çürüttü** — bu teşvik edilen davranış.
2. **Bir performans sayısı, ölçüldüğü DB'nin istatistikleri kadar gerçektir.** M6-03'ün `MATERIALIZED` CTE
   gerekçesi (**27×**) yeniden üretilemedi: veritabanı **hiç `ANALYZE` edilmemişti** (`n_live_tup` 5.326 /
   gerçek 111.167). `ANALYZE` sonrası değiştirilen şekil **~4× daha hızlı** çıktı. **27× geri çekildi**, çit
   **başka bir gerekçeyle** tutuldu ve **geri alınabilir** biçimde yazıldı.
3. **Sayı/iddia-etiketi sınıfı SEKİZ kez ısırdı ve mekanizması bulundu.** M6-02'de beş, M6-03'te üç. Sonuncuda
   yapıcı üç düzeltmeyi *"yapıldı"* diye raporladı; ölçüm **2'de 1, 0'da 3, 0'da 2** dedi — sebep: düzeltmeleri
   yapan **betik ilk `assert`'te ölmüş** ve yapıcı betiğin **niyetini** raporlamış. **Kural: `grep`/`shasum` ile
   SONUCU doğrula.**

**Sevk edilmiş iki kusur bulundu ve düzeltildi.** `.docket-label` **3,13:1** ile sevk ediliyordu (AA 4,5:1),
**12 çağrı yerinde**, **çalışanın tap ekranı dahil** — ve yanında beş ton daha. Yıllardır görünmemesinin sebebi
üründe **otomatik kontrast ağı olmamasıydı**; artık var ve **CI'da koşuyor**. M6-03'te filtre çubuğu sayfanın
**%96'sıydı** (867 KB'ın 835'i, sınırsız büyüyen) → kullanıcı kararıyla metin kutusuna döndü, **32 KB**.

**Ne kaldı.** M6-04 (FLAGGED onay kuyruğu, **§4.3**) — ve devraldığı en ağır şey: *"düzeltme = yeni kayıt +
`audit_log`"* kuralının **`audit_log` yarısı hâlâ yok** (ölçüldü: 408 manuel satır, **0** audit satırı, manuel
giriş rotası **yok**).

### 2026-08-03 (5. oturum, devam) — **M6-01 KAPANDI (A+B)** · panel girişi · **18 tur, 8 RED — projenin en uzun görevi**

**Ne yapıldı.** M5-09/M5-10/M5-11 ile M5 kapandı (11/11; ayrıntı dosyanın başındaki bloklarda), sonra
M6-01 **A/B kalıbıyla** bölündü ve ikisi de sevk edildi: A (`66d5442`) global admin çözümlemesi + şema
sertleştirmesi, B (`4bc2e72`) bcrypt + oturum + iki ekran + oran sınırı + `audit_log`. 00011'in **beş
yükümlülüğünün beşi** karşılandı ya da limit yazıldı. **12 limit** devredildi.

**Ne öğrenildi — bu oturumun tek büyük dersi.** *Bir düzeltmenin kendi ağı AYNI TURDA ölçülmezse, düzeltme
yazılmamış sayılır.* Beş koruma sevk edildi ve **beşinin de silinmesi suite'i yeşil bıraktı**; hepsi ayrı
turda, ayrı denetçi tarafından, **mutasyonla** bulundu. İkinci ders: **iki mercek birbirinin yerine
geçmiyor** — genel üçüncü göz 5. turda ONAY verdi, `tappa-security-auditor` hemen ardından **sömürülebilir
bir 53× zamanlama kehaneti** buldu. Üçüncüsü: **sayı hijyeni kendi başına bir bulgu sınıfı** (altı vaka);
dar bir bant üç kez yazılıp üç kez tutmadı, format **gözlenen aralığa** çevrilince bitti.

**Orkestratör kendi hatasını da kaydediyor:** 12. turda denetçinin sunduğu iki seçenekten *"bütçeyi ekle"*yi
seçtim ve gerekçem reponun kendi sırasıydı — ama `tap.go`'nun **ByAddress → Identify → BySession** deseninin
yalnız **ilk aşamasını** uygulattım. Sonuç 13. turda ölçüldü: flood kapısı **çıkışı reddedip oturumu canlı
bırakıyordu**, ve sunucu tarafı süre olmadığı için o pencerede oturumu bitirecek hiçbir yol yoktu. *Bir
deseni kopyalarken kaç parçası olduğunu saymak, hangi parçayı kopyaladığını bilmekten daha önemli.*

**Ne kaldı.** M6-02 (docket iskeleti) — ve **üç sayıyı devralıyor**: `adminSessionLimit` (kopyalandı,
türetilmedi, iki tavandan **dar** olanı), `adminFloodLimit` (kimliği doğrulanmış yüklemeleri de taşıyor),
`sessionGate`'in sınırladığı iş (bugün **boş**, M6-02 dolduruyor).

### 2026-08-01 (5. oturum, devam) — **M5-08 done** · QR kanalı · **debounce dört katmanda sertleştirildi**

`1d836e3`. İki denetçi ONAY, **8 tur, 7 RED**. Görev QR'ı **kanıtlamakla** başladı (motor zaten bağlıydı),
ölçüm bir zincir açtı ve **karar motoru değişti**.

**Zincir, ve her halkası ancak öncekini kapatınca göründü.** §5 satır 5 debounce'u — sayacı olmayan bir
kanalın **tek freni** — dört şekilde aşılabiliyordu: **mesafe** (gap istemcinin `occurred_at`'inden) →
**seçim** (`ORDER BY occurred_at DESC`, yani öncülü de istemci seçiyor) → **işaret** (ileri tarihli tap
reddedilir ama **kaydedilir**, sıralamayı kazanır, negatif gap guardrail'i **tümden kapatır** → sonraki
dürüst tap'ler `flag` değil **`ok`**) → **eşzamanlılık** (50 eşzamanlı POST, 0,48 sn, **51 sayılan satır**).
Kullanıcı iki kez *"şimdi düzelt"* dedi: **iki koşullu debounce** ve **kişi başına advisory lock**. ADR 0006.

**Bir düzeltmenin kendisi meşru bir akışı kırabilir.** Düz `min` kuralı, müdürün geçmişe tarihli girişinden
30 sn sonra gelen **çalışanın gerçek tap'ini** yutuyordu → `created_at` bacağı yalnız tap kanallarında.
Ve çevrimdışı kuyruğu (M9-01) gerçekten kırıyor: **ifşa edildi, gerekçelendirilmedi**.

**🔬 İki YÖNTEM dersi, ikisi de bir denetçinin/yapıcının ölçümünü çürüttü:**
1. **Genel denetçinin "kilit masumdur" kontrol grubu artefakttı** — flood'u **tek oturumdan** sürdüğü için
   istekler `BySession` 300/10dk'ya takılıp kilide **hiç dokunmadan** 429 alıyordu (200 istek 40 ms'de bitti).
   Temiz A/B (flood **ayrı oturumlardan**, kurban **tek atış**): ilgisiz kişinin gecikmesi **6–9×**, ve
   `pg_stat_activity` doğrudan gösterdi: **16 bağlantının 15'i** `wait_event='advisory'`, kontrol kolunda **0**.
2. **Yapıcının "0 hata" yarış ölçümü** konfigürasyon başına **tek örnek** aldığı ve sondanın **id-verme
   şekli üretimden farklı** olduğu için yanlıştı. Doğruyu ancak **gerçek kod yolunu** sürerek buldu.
   *(Ben denetçinin "harness ROLLBACK ediyor" hipotezini olduğu gibi aktarmıştım; yapıcı `WithTenant`'ın
   commit ettiğini gösterip **ölçümle itiraz etti** ve haklıydı.)*

**Ve kalıcı ders:** 7 RED'in tamamı *"bir cümle, sistemin vermediği bir şeyi beyan ediyor"* sınıfından;
son üçü **yalnız metin**. Aynı iddia (*"birkaç milisaniye"*) **altı kez** yeniden doğdu — bir kez **aynı
commit içinde** bir dosyada geri çekilirken kardeşinde ayakta kaldı. Yapıcı bir turda bunu düzelttiğini
raporlayıp düzeltmediğini de **kendi buldu** (betiği assert'e takılmış, grep'i geri-çekme metnini eliyordu).

**Sırada:** M5-09. ⚠️ **Önce seed'in `aes_key_ref`'ini KEK-sarmalı yap** — yoksa NFC yolu 500 verir ve
"bir günü simüle et" çalışmaz (QR yarısı bugün çalışıyor). Ve **`make db-reset`**: benchmark 20 002 satır bıraktı.

### 2026-08-01 (5. oturum, devam) — **M5-07 done** · aktivasyondan onay ekranına akış tamam

`e0a5700`. İki denetçi ONAY, **2 tur**. Yeni olan: `GET /activate/tour?step=1..3` — üç slayt,
**sunucu render, JS yok, istemci state'i yok**, her slayttan atlanabiliyor, ve **hiçbir şey yazmıyor**
(7 istek boyunca satır sayıları donuk + pozitif kontrol). `Submit` ilk aktivasyonu tura, **ikinci
cihazı** doğrudan onaya gönderiyor — çünkü zaten tap etmiş birine *"ilk tap'in deneme"* demek gerçek
bir check-in'i deneme sandırırdı.

**Görevin yarısı zaten hazırdı ve asıl iş bunu ÖLÇMEKTİ.** Ölçüm iki **maskeli mutant** buldu:
`gather`'ın `if !open.Practice` guard'ı silinince **tüm suite yeşil** kalıyordu (motor tarafı
`decide.go`'da bağımsız kanıtlı olduğu için), ve `transaction()`'ın `Practice` eşlemesi **eşdeğer
mutanttı**. **Oturumda üçüncü kez aynı şekil:** bir garanti A paketinde kanıtlanıp B'de tüketiliyorsa,
**B'nin onu kullandığı ayrıca pinlenmeli**.

**Tek RED de aynı aileden ve öğretici:** bir test yorumu *"a link ADDED to a slide fails too"* diyordu;
`assertRefs` href **DEĞER** kümesini karşılaştırıyor, **sayısını değil** → slayta **metinsiz**, hedefi
zaten izinli üçüncü bir `<a>` eklenebiliyordu: görünmez ikinci bir dokunma hedefi, tam da §9'un baktığı
şey. Düzeltme slayt başına **sıralı (hedef → etiket)** listesini pinliyor.

**Vaat ölçüme indirildi (§4.6).** Practice hakkı **herhangi bir** önceki satırla harcanıyor
(`GetLastTransactionForEmployee`'de verdict ve kanal yüklemi yok), o yüzden slayt *"your **first** tap"*
diyor ve *"Whatever a tap turns out to be, the screen right after it says so"* ile kapanıyor. Güvenlik
denetçisi `practice`'i **dört kanaldan** (query · header · multipart · JSON) hem iddia hem **reddetme**
yönünde denedi ve sütunu geri okudu: `practice=false` gönderen ilk tap yine **`true`**.

**Ve ileriye dönük iki kart düzeltildi** — bu oturumun kalıcı dersi: *bir görevi kapatan değişiklik,
o davranışı tarif eden **ileri** kartları da kapatmak zorunda.* M6-07 ve M6-11'in "çıkışsız açık kayıt"
kriterleri practice istisnasını saymıyordu → M6'da **her yeni çalışanın deneme tap'i** müdürün "eylem
gerekiyor" kuyruğunda belirecekti. (Aynı sınıf bir tur önce `m6-dashboard.md:56`'da yaşandı ve bu
oturumda `state.md`'de **iki kez** benim hatam olarak çıktı.)

**Sırada:** M5-08 (QR kanalı). ⚠️ Ana tuzağı yazılı: **QR'da ilerletilecek sayaç yok**, tek savunma
60 sn person-scoped debounce.

### 2026-08-01 (5. oturum, devam) — **M5-06 done** · onay ekranı bitti · **15 tur, 11 RED**

`b3fb2b5`. İki denetçi ONAY (`tappa-security-auditor` + genel üçüncü göz). **Projenin en uzun görevi**, ve
sebebi öğretici: iş bittikten sonra **on bir turun tamamı korumanın kendisiyle** geçti.

**İlk iki RED ekranın metnindeydi ve ikisi de §4.6.** `ignored` ekranı *"Your earlier tap stands."* diyordu;
debounce **verdict'ten ve kanaldan bağımsız** çalıştığı için öncül bir `flag` (onay kuyruğunda, saate
girmemiş) olabilir → görevin `flag`'den sildiği sessiz onay kusuru **yok olmamış, `ignored`'a taşınmıştı**.
`reject` başlığı sayfanın **en büyük yazısında** *"Not recorded"* diyordu; oysa `Record` INSERT'ten sonra
hiç hata döndürmüyor, yani render edilen bir Result sayfası **satırın kanıtı** — ve aynı sayfa dört satır
altta "was recorded" diyordu. **Yanlış cümleyi hiçbir test yasaklamıyordu.**

**Kalan dokuz RED'in hepsi DÜZELTMENİN içinden çıktı.** Her koruma bir sonraki turda yenildi: elle kurulmuş
bayt-golden üretimin **hiç render etmediği** bir gövdeyi pinliyordu (Note'suz 971 B vs gerçek 1061 B) →
metin-düğümü listesi CSS `content`, `</main>` dışı, `aria-label` ve `title` ile yenildi → `<input readonly
value>` (*"value machine-facing'dir"* gerekçesi yanlıştı) → `<iframe srcdoc>`/`<object data>`/`<img
src=data:svg>` → `<link href="data:text/css,…{content:'…'}">` (izinli eleman, okunmayan öznitelik) →
metin testi retry dalını **hiç render etmiyordu** → `<meta http-equiv=refresh>` → ve o kontrolün regex'i
**öznitelik sırasına bağlıydı**, yani oturumun kanonik dersi (*kontrol ile tüketici aynı temsili görmeli*)
**kendisini enforce etmek için yazılmış kontrolün içinde** tekrarlandı.

**Kırılma noktası 11. turdaydı ve bir mekanizma değil bir KURAL'dı:** *yeni kanal kapatılmıyor, dürüstçe
LİMİT olarak sayılıyor; ve "tamamen/bitmiş/complete" yazmadan önce onu yenmeye çalış.* İş üç tur sonra
bitti. Bugün **8 kanal limit olarak yazılı** — kapatılamayanı saymak, kapatıldığını iddia etmekten güvenli.

**Sonuç:** üç dar beyaz liste (görünür metin + 11 öznitelik · eleman adları, **kapalı küme 16/14, iki yönlü
eşitlik** · dış referanslar, `{/static/css/app.css}`) + 7 tap yanıtında pinlenen CSP. Markanın *"mutlak URL
sayısı 0"* kuralı **ilk kez teste bağlandı**. Ayrı bir wiring boşluğu da kapandı: altı hata ekranının
metnini yalnız elle kurulmuş bir view pinliyordu → `renderProblem` saptırılınca artık **RED 20/17**.

**İki süreç dersi (ikisi de `agent-brief.md`'ye yazıldı):**
- **Denetçi mutasyonunu `git checkout` ile geri ALMAZ** — bir denetçi commit edilmemiş **12 satırı kalıcı
  sildi**; bir başkası yedeklerken `basename` çakışmasıyla dosya ezdi (`git hash-object` ile birebir
  kurtardı ve **kendisi bildirdi**).
- **Yapıcının kendi hatasını bildirmesi ve yanlış bir talimata ölçümle itiraz etmesi iki kez iş kurtardı:**
  bir yeşil kalan mutasyonu kendi buldu, ve *"büyük harfli meta `.templ`'den üretilemiyor"* diye gelen
  düzeltme talimatını `make templ` çıktısıyla çürüttü (templ büyük harfi **birebir koruyor**).

**Sırada:** M5-07 (mini tur + practice tap). `practice` bayrağı ve TRAINING damgası zaten çalışıyor;
M5-07'nin işi turun kendisi.

**Aynı gün, kapanış:** damga kontrastı kullanıcıya soruldu ve **karar alındı — *kelime `ink`, durum rengi
çerçevede*.** Eşleme korundu, palete yeni token girmedi, beş damga da AA'yı geçiyor (13.27–15.55; önce
render edildiği hâliyle **üçü** altındaydı). `opacity-80` kaldırıldı. **skill `tappa-brand` güncellendi** —
skill'in kendi *"Kontrast AA"* kuralını iki damgada çiğnediği açıkça yazıldı. Ve bu küçük iş **iki ders**
üretti: (1) **Tailwind `.templ`'i YORUMLAR DÂHİL ham metin olarak tarıyor** — düzyazıda geçen `opacity-80`
gibi bir kelime **gerçek ölü kural derliyor** (ölçüldü: iki isim = **+330 bayt**), ve tuzak **zaten
ateşlenmiş**: `app.css` bugün **yedi** ölü kural taşıyor (`.filter` 185 B · `.visible` · `.relative` ·
`.min-h-16` · `.static` · `.fixed` · `.hidden` = **334 bayt, %2.34**), hiçbiri **95 `class` özniteliğinin**
hiçbirinde yok. (2) **Bir değişiklik `state.md`'yi yanlışlayabilir:** denetçinin bloklayan bulgusu koda
değil **bu dosyaya** çıktı — kontrast satırları düzeltmeden önce hâlâ *"kullanıcı kararı bekliyor"* ve
*"1.52:1 / opacity:.8"* diyordu. **Ders: bir görevi kapatan değişiklik, o görevin devir notlarını da
kapatmak zorunda** — yoksa sonraki oturum var olmayan bir kusuru taşır.

### 2026-07-31 (5. oturum) — **M5-05 done** · 🎉 **uçtan uca check-in ÇALIŞIYOR**

`b82c9f2`. İki denetçi ONAY. **M3+M4 ilk kez gerçek bir HTTP isteğiyle çalıştı** — policy motoru,
10 guardrail ve `tap.Decide` bugüne kadar yalnız saf paket olarak, tablo testleriyle kanıtlanmıştı.
**§5'in yedi satırı da uçtan uca kanıtlandı**; güvenlik denetçisi altısını **kendi HTTP sondasıyla**
yeniden üretti.

**Ölçülmüş bloklayan — ürünün en sık yolunda saklıydı.** Seed'li tenant'ta `policies`/`policy_versions`
**0/0**'dı → baseline katmanlı kararlar `23503`. Guardrail satırları (§5 1–5) **yazılabiliyordu**,
çünkü onlar `policy_version_id NULL` taşıyor. Yani delik tam olarak **satır 6 ve 7'deydi: sıradan `ok`
ve `flag`, ana yol.** M7-03 hiç çalışmamıştı. Çözüm: **ilk ihtiyaçta idempotent materyalizasyon**
(uuid-v5 türetilmiş id → conflict hedefi **var olan kısıt**, migration yok; append-only korunuyor).
"Gürültülü başarısız ol" alternatifi ya tap'i düşürmeye (§4.6) ya da o tenant'taki **her** tap'i
review'a göndermeye indirgeniyordu — ikisi de ölçümle gösterildi.

**🔴 N5 KAPANDI.** `tap.Input` iki tenant'ı taşıyor, `sys:tenant-mismatch` ateşliyor: **403**, iki
tenant'ta da **0 satır**, gövdede yabancı id/uid/mekân adı yok (denetçi yabancı mekâna "SECRET RIVAL
VENUE" adını verip sıfır sızıntı ölçtü), ve kararı **guardrail** veriyor — FK değil.
İki denetim deliğin kendisinden fazlasını buldu:
- **F1:** `advance`, karşılaştırmadan **ÖNCE** ve **yabancı tenant'ın RLS bağlamında** koşuyordu →
  çapraz-tenant tap yabancı `last_ctr`'ı **900→901** yapıyor, o tenant'ta **hiç iz bırakmıyordu**.
- **Yapıcı bizim N5 ifademizi düzeltti:** beslenmemiş hâlde *yazma* çapraz-tenant `ok` **üretmezdi** —
  `transactions_tag_fk` **23503** → 500 → **kayıt kaybı**. Şema **sessiz bir ikinci ağdı** ve bir
  izolasyon ihlalini §4.6 kaybına çeviriyordu. İkisi de ölçüldü.

**🔁 En değerli bulgu bir KANIT boşluğuydu — ve bu sınıf oturumda üçüncü kez çıktı.** Güvenlik
denetçisi üretim yazma yolundaki atomik ilerletmeyi **gerçek bir TOCTOU'ya** çevirdi ve
**tüm suite yeşil kaldı**. Sebep: `internal/sun` `AdvanceCounter`'ın N yarışçıyı tek kazanana
indirdiğini kanıtlıyor, `checkin` tap'in doğru kayıtla bittiğini kanıtlıyor — ama **tap yolunun onu
ÇAĞIRDIĞINI** pinleyen hiçbir şey yoktu; mutasyon **ikisinin arasından** geçti. Yeşil kalmasının tek
sebebi `SELECT→UPDATE` penceresinin milisaniye-altı olmasıydı: 80 ms'ye genişletilince **12/12 tap
SUN-valid** oldu.
> **Doğru çözüm yarışı daha çok test etmek değil, ÇAĞRIYI PİNLEMEKTİR.** Yapıcı bunu iki kez
> ölçtükten sonra seçti (bariyerli ve bariyersiz HTTP yarışı, ikisi de bozuk şekle karşı yeşil kaldı):
> tüketici tarafında sayan bir arayüz → "tam 1 `AdvanceTagCounter`, şu tenant/uid/ctr ile, tag
> tenant'ının `WithTenant`'ı içinde". Denetçinin mutasyonu birebir kurulunca artık **RED**.
> agent-brief'e yazıldı.

**Üç ölü/yanlışlanamaz koruma daha bulundu:**
- **`sys:tap-freshness` ÖLÜYDÜ** — `tap:pageAgeSeconds` hiç beslenmiyordu. Beslendi. Bandı
  varsayılanlarla **boş** (TTL 900 == eşik 900) — **hiçbir testin yakalamamasının sebebi buydu**;
  M5-10 pencereyi daraltınca erişilebilir olacak. İki cevap **türce farklı**: TTL kaydedilmeyen 400,
  guardrail **kaydedilen reject** (§4.6).
- **N3 wiring'i yanlışlanamazdı:** harness debounce'u `DefaultParams()` ile **aynıydı** → wiring silinince
  test yeşil kalıyordu. **Dejenere değer**, "kendi kurduğu nesne"nin akrabası. Harness 120 sn'ye çekildi.
- **Dört verdict damga sınıfı derlenen CSS'e hiç girmiyordu** — Tailwind globları **Go dosyalarını
  taramıyor** ve `StampClass()` repodaki tek Go-tarafı sınıf literaliydi. `app.css` gitignore'da olduğu
  için hata yalnız **taze build'de** görünüyordu; denetçinin yöntemi bu yüzden buldu.

**Dayanıklılık (denetçi ölçümü):** 9 düşmanca girdi şekli (yıl 9999, yıl 1, ±23:59 offset, ±180
koordinat, üstel ve denormal float) → **hepsi 200 + tam bir satır, hiç 500 yok**; **politika katmanı
zorla düşürülünce kayıt YİNE yazılıyor** (`flag`/`default`). §4.6 tuttu. Dokümanlar `redirect`/`ignore`
beyan **edemiyor** → bir tenant politikası tap'i **susturamaz**.

**Ayrıca:** sıfır-zaman nöbetçisi `0001-01-01T00:00:00Z` ile çakışıyordu (denetçi **bir saniye
ötedeki pozitif kontrolle** gösterdi) → `OccurredAt` **pointer** oldu, "beyan edildi mi" ile "neydi"
artık aynı değeri paylaşmıyor.

**Yöntem tuzağı (agent-brief'e yazıldı):** çıplak `go test ./...` **her DB testini sessizce SKIP**
ediyor; yalnız `make test` gerçek Postgres'e karşı koşuyor. "0 SKIP" iddiası hangi komutla ölçüldüğü
söylenmeden anlamsız.

**Sıradaki: M5-06** (onay ekranı) — M5-05 geçici bir `pages.Result` bıraktı.

### 2026-07-31 (5. oturum) — **M5-04 done** (tap sayfası)

`cfa6cd5`. İki denetçi, üçüncü göz **RED**. **Kullanıcı kararı:** buton nötr **"Tap"** kalıyor
(yön karar motorunun işi; sayfada tahmin etmek onay ekranıyla çelişirdi — §9 gereği soruldu).

**§4.4 — kart mevcut API ile imkânsız bir şey istiyordu.** "Sayfa açılışında sayaç ilerletilmez" ama
`sun.Verify` 6. adımda ilerletiyor → yeni giriş noktası `PreviewWithoutReplayProtection`. Caydırıcılık
**isimle değil yapıyla**: `Preview` ≠ `Result` (atanamaz → `Verify`-şekilli kod **derlenmez**),
`SUNValid` alanı **yok**, ve denetimden sonra `db.ResolvedTag` da taşınmıyor — çünkü
`pv.CMACValid && p.Ctr > pv.Tag.LastCtr` **yazılabilir bir cümleydi** (§4.4'ün adıyla yasakladığı
TOCTOU) ve KEK-sarmalı anahtarı handler'a veriyordu. **İki denetçi de beş caydırıcıyı kendi
paketlerinden derleyerek denedi; hepsi derleme hatası.**

**🔬 Güvenlik denetçisi yapıcının bıraktığı boşluğu ölçümle kapattı.** Yapıcı "HTTP yolunda
geçerli-CMAC testi yok" diye dürüst bir sınır yazmıştı (gerekçe: SDM türetmesinin ikinci kopyası
M2-04'ün byte-reversal hatasını gizleyebilirdi). Denetçi tam da doğru şeyi yaptı — `internal/sun`'dan
**tek satır almadan**, repo dışında **bağımsız RFC 4493 CMAC + NXP SDM** yazıp **gerçek geçerli bir SUN
URL'i mintledi**: 30 açılış → `last_ctr` **0→0**; aynı URL `Verify`'dan geçince **700→701,
`SUNValid=true`**; replay → false. **İç-tutarlı vektörün yakalayamayacağı sınıf ilk kez dışarıdan
sınandı.**

**RED — ve bu oturumun en öğretici hatası, çünkü kimsenin dosyasında değildi.** `NewTap`
`TapLimiter`'ı **`Audit` olmadan** kuruyordu → M5-03'ün **onaylanmış** kriteri ("429 + tenant
çözülmüşse `audit_log` satırı") **üretimde ölüydü**: 15×429, red **kimliği çözülmüş**
(`employee_id=…0301`), `audit_log` **4145→4145**. M5-03 yeteneği teslim etmiş, **montajı devretmişti**;
yanlışlanan cümleler M5-03'ün **kendi dosyasında ve kartında**, doğru göründükleri hâlde duruyordu.
`cmd/tappa/main.go`'da recorder zaten vardı — `NewTap`'in imzasında parametre yoktu, yani unutulmuş
satır değil **eksik tasarım**.

> **🔁 Ve düzeltmenin ilk mutasyonu YEŞİL kaldı.** Red testleri `tp.limiter`'ı **kendileri** kurup
> `Audit`'i açıkça geçiriyordu — yani üretim montajını hiç sınamıyorlardı. Yapıcı bunu **kendi
> raporladı**: *"denetlediği şeyi kendisi kuran test hiçbir şey denetlemez."* Testler artık ürünün
> kurduğu limiter'ı **üretim bütçesiyle** sürüyor. Kapanış denetiminde **aynı tuzak bir alan yanında**
> bulundu (`Refused: nil` de tüm suite'i yeşil bırakıyordu) → o da kapatıldı. agent-brief'e yazıldı.

**İmzalı bağlam:** çipin MAC'i sunucuda **bir kez** kontrol ediliyor, sayfaya **tek bit** geçiyor;
türetilmiş anahtar + oturum id'si AAD olarak. Denetçi şemayı **kendi HMAC'iyle** yeniden kurdu ve
birebir eşleşti; **dokuz sahtecilik denemesi** reddedildi; sabit zamanlı; TTL 15 dk, ileri kayma 1 dk
fail-closed. **`ctr` ve uid bağlamda seyahat ediyor** (ikisi de adres çubuğunda zaten var), **CMAC
etmiyor** — tersini iddia eden **üç yer** düzeltildi.

**Bilinçli sapma (denetçi meşru buldu):** sayfa ön-doğrulama başarısız olsa da render ediliyor
(retired/lost/bozuk CMAC/yabancı plaket). Reddetmek butona hiç basılmaması demek ve §4.6'nın
**kaydedilmesini istediği** bir `reject` iz bırakmadan kaybolur. Yabancı-tenant senaryosunda **mekân
adı gövdede 0 kez** geçiyor (yalnız UUID'ler, opak).

**Ayrıca:** fontlar self-host (6 woff2, **79.032 bayt** — "92 KB" ölçüm hatasıydı, dizinin tamamıydı;
SKILL.md'de de düzeltildi), **mutlak URL 0** · `/static` dizin listelemesi kapatıldı · tap yanıtlarına
**CSP** · `watchPosition` **0**, çift-basış artık `preventDefault` (önce koordinatsız natif submit
oluyordu = kanıt kaybı) · `internal/domain/tenant` yeni paket, tüketici arayüzü **yalnız `WithTenant`**
tanımlıyor (RLS dışı okuma **yapısal olarak** mümkün değil).

**M5-05'in kart akışı düzeltildi:** "parse → `sun.Verify` → …" gönderilen tasarımda **çalışmaz**
(bağlamda CMAC yok → `verifyMAC` false → sayaç hiç ilerlemez). Gerçek sözleşme yazıldı. **Sıradaki:
M5-05** — milestone'un en kritik görevi; M3+M4 ilk kez gerçek bir istekle çalışacak ve **N5 orada
kapanmalı**.

### 2026-07-31 (5. oturum) — **M5-03 done** (middleware: gerçek IP, kimlik, oran sınırı)

`1fdd1ad`. `internal/httpx/{realip,identity,ratelimit}.go`. **İki denetçi, 2 RED.**

**Çözücü:** XFF **sağdan sola**, tüm başlık örnekleri boyunca; güvenilmeyen peer → başlık **hiç
okunmaz**; `TrustedProxies` boş → `RemoteAddr`. chi `middleware.RealIP` **kullanılmadı** (başlığa
koşulsuz güvenir). Gerekçe §5: IP eşleşmesi **50 güven puanı**, yani **sahtelenebilir bir adres hiç
adres olmamasından kötüdür**. Denetçi kendi 24 satırlık tablosu + 10 sahtecilik denemesi + **canlı TCP
soketi** ile sınadı: obs-fold, başlık büyük/küçük harf, 51 hop, 100k girişli zincir, 4-in-6, zone —
append eden proxy arkasında **10/10** sahtecilik taze kova satın alamadı.

**RED-1:** *"tek otorite / bir handler kazara ham başlığa uzanamaz"* **yanlıştı** — `Forwarded`
(RFC 7239), `CF-Connecting-IP`, `X-Client-IP` handler'a ulaşıyordu. Strip listesi **3→32**; canlı
soketle ölçüldü: **36 adayın 23'ü** geçiyordu → **4**. Kalan dördü **pozitif kontrol**: `Via` ve
`X-Forwarded-Host/-Proto` adres taşımıyor, **`Origin` ise CSRF kontrolleri için taşıyıcı** — silinse
aktivasyon kırılırdı. İddia denylist kapsamına indirildi (bilinmeyen satıcı başlığı hayatta kalır).

**RED-2 — ve bu ders bu oturumun en pahalısı:** varsayılan-rota kapısı **ham** prefix'e bakıyordu, ama
normalizasyon 4-in-6'yı unmap ediyor → `TAPPA_TRUSTED_PROXIES=::ffff:0.0.0.0/96` kapıya `/96` görünüp
çözücüde **`0.0.0.0/0`** oluyordu. Prod'da **ne hata ne uyarı**; sıradan bir internet çağıranı kendi
adresini yazabiliyordu. **Kapı bir önceki RED'e cevaben eklenmişti.**
> **🔁 Desen (üçüncü kez):** `HTTPS://` (M5-01) · `Cross-Site` (M5-02) · `::ffff:` (M5-03) — hepsinde
> **kontrol, tüketicinin gördüğünden farklı bir biçime bakıyordu.** agent-brief'e yazıldı.

Çözüm **ikinci temsili silmek** oldu (config v4-mapped yazımı reddediyor, httpx düşürüyor) — iki
kanonikleştirme hatanın **kaynağıydı**; ayrıca `config→httpx` **import döngüsü** yüzünden
normalizasyon config'e taşınamıyordu. Yapıcı iki alternatifi de gerekçeleyerek reddetti.

**Kart iki yerde düzeltildi (gerçekle hizalama, kaçış değil — denetçi ikisini de meşru buldu):**
klasik bir **tenant middleware'i tap yolunda KURULAMAZ** — tenant çözümlemenin **ÇIKTISI**dır (ADR 0002
md.7); girdi alan bir middleware çağıranın **kendi tenant'ını adlandırmasına** izin verirdi. Yerine
`httpx.Identify` yalnız **gerçekleri** taşıyor ve **sıfır değeri `SessionUnresolved`** (M5-01'in kutup
dersi): middleware'i unutan bir rota **gerçek bir oturumsuz tap gibi görünemez**. `BySession` o durumda
**500** veriyor (fail-open'dı, denetçi 100/100 isteğin ölçülmeden geçtiğini ölçtü) ama `SessionAbsent`
**geçiyor** — §5 satır 3 meşrudur. İkincisi: 429'da `audit_log` **yalnız kimlik çözüldükten sonra**
mümkün (`tenant_id` NOT NULL + FK) → **uydurma "sistem tenant'ı" üretilmedi**.

**M5-02'nin adlandırılmış yükümlülüğü kapandı:** `handler.clientIP` artık `httpx` üstünden çözüyor →
proxy arkasındaki çağıranlar **ayrı kova** (negatif kontrol: çözücüsüz = M5-02 hâli → B'ye 429).
`floodLimit` **600'de bırakıldı** — düzeltilmesi gereken **sayı değil anahtardı**.

**Yapıcının kendi testi kendi taslağını çürüttü:** `tapSessionLimit` ilk hâlinde 120'ydi; 5 saniyelik
bir yenileme döngüsü tam 120 eder → 300'e çıkarıldı. Aynı sınıf: "meşru akış sınıra değmez" cümlesi
ölçülmeden yazılmamalı.

**Kapsam:** `TapLimiter` yazıldı ve testlendi ama **monte edilmedi** (`/t`, `/api/checkin` yok) —
montaj sırası bir **sözleşme** ve 429'un §4.6 kalıntısı adıyla yazıldı (aşağı "M5-04/M5-05'e
devralınan"). **N5 yalnız oturum yarısı** teslim edildi; `sys:tenant-mismatch` hâlâ ölü ve **"kapandı"
denmiyor** — tag yarısı M5-05.

**Sıradaki: M5-04** (tap sayfası, skill `tappa-brand`) — bağımlılıkları tamam.

### 2026-07-31 (5. oturum) — **M5-02 done** (davet + aktivasyon, iki fazda)

Görev büyük olduğu için **ikiye bölündü**: A fazı veri katmanı (`9139ee7`, agent `tappa-db-migrator`),
B fazı akış + arayüz (`0601b6d`). Toplam **5 denetim turu, 3 RED**. Ayrıca `00010 locations.wifi_ssid`
(Q14 WiFi adımı için — kart "ağ adı lokasyon kaydından gösterilir" diyordu ama **öyle bir alan yoktu**).

**Kullanıcı kararları (2026-07-31):** GDPR saklama süresi **config'den** gelsin, koda hukuki sayı
gömülmesin (→ `TAPPA_RETENTION_YEARS`, Q13 hukukçu onayı **backlog B3**) · WiFi ağ adı için
**lokasyona alan eklensin** (→ 00010).

**Üç RED — üçü de oturumun tekrarlayan sınıfı** (*dosya/kart, sağlamadığı garantiyi beyan ediyor*):
1. **A:** kart, `code_hash` biçim CHECK'inin **kod entropisini zorladığını** ve kısa koda geçişin CHECK'i
   tetikleyeceğini yazıyordu. Ölçüm: `sha256('123456')` da 64-hex → **tel-tuzak hiç ateşlenmez**.
   Yükümlülük düzeltme bloğundan **Kabul kriterleri**ne taşındı (≥128 bit değilse kilitleme **zorunlu**,
   ve *hiçbir mekanik kontrol bu geçişi yakalamaz* açıkça yazıldı).
2. **B:** **aktivasyon-fixation.** `SameSite=Lax` çerez **gönderimini** kısıtlar, **yazılmasını değil.**
   Çapraz-site GET saldırganın kodunu kurbanın tarayıcısına ekiyor → sonraki aynı-site GET **başka
   tenant'ın** formunu render ediyor → `Submit` mevcut oturuma **hiç bakmadığı** için kurbanın oturumu
   **sessizce eziliyor**. Oysa dosya "en kötüsü saldırganın zaten elindeki kodu harcamaktır" diyordu.
3. **B:** **sınırsız, SİLİNEMEZ `audit_log` yazımı.** İki dal her istekte satır yazıp hiçbir pencereyi
   artırmıyordu: 300 istek → 290×429 **ama 300 satır**. `audit_log` DB seviyesinde append-only —
   **`tappa_owner` bile silemiyor** — ve ön koşul yalnızca *bir ölü davet linki* (her aktive çalışanın
   WhatsApp'ında duran kendi linki). **§4.6'nın koruduğu iz, kendi bağışıklığı yüzünden silah oluyordu.**

**Bunu düzeltmek dördüncü bir sorunu açığa çıkardı** (denetçi YÜKSEK/bloklamayan dedi, yine de kapatıldı):
tek per-IP penceresi **başarıları da** reddediyordu → 60 bilinmeyen kod, geçerli bir aktivasyonu
kilitliyordu. `clientIP` `X-Forwarded-For` okumadığı için ters proxy arkasında **her istek tek anahtarı
paylaşır** → tek çağıran tüm ürünü kapatabilirdi. Çözüm **üç bütçe, üç iş**: `flood` (yalnız bu
geçerli bir aktivasyonu reddedebilir) · `unknown` (yalnız **süreç log'unu** sınırlar) · `invite`
(yalnız **`audit_log`'u** sınırlar). Ölçüm: 300 istek → **11 satır**, 60 bilinmeyen kod sonrası geçerli
kod **303 servis edildi**, 605. istek yine **429**.

**Ders — ayrım cümlenin içinde saklıydı:** *"meşru akış sınıra yapı gereği değmez"* akışın **kendi
katkısı** için doğru (200 ardışık başarılı aktivasyon → sıfır 429), akışın **servis edilip edilmediği**
için yanlış. İki yarı ayrı ayrı yazıldı.

**Yapısal kararlar (hepsi ölçümle, tercihle değil):**
- **Kaynaşık CTE** `ConsumeInviteAndActivate`: iki ayrı sorgu "aktive ⇒ davet tüketildi"i **çağrı
  sırasına** bırakıyordu (hayalet-çalışan). Kaynaştırmak **daha ince bir hatayı** ortaya çıkardı:
  veri-değiştiren CTE **koşulsuz** çalışır → deaktif çalışanda davet **yanıyordu** (COMMIT ile ölçüldü).
  İç CTE'ye `EXISTS` guard'ı → `burned=f`. **Yapısal > disiplin.**
- **`FOR SHARE` ölçümle reddedildi:** kalan dar yarışı kapatırdı ama iki cihaz aynı çalışanı aktive
  ederken **40P01 deadlock** üretiyor. Harcanmış bir kod, 500'den iyidir.
- **Sütun-düzeyi `GRANT UPDATE (used_at)`:** dosya "tablo-geneli UPDATE **şart**" diyordu; ölçüm
  çürüttü. Diriltme (`expires_at`), kaydırma (`employee_id`), hash-yeniden-yazma → **permission denied**.
- **Alan ayrımı çift:** ayrı anahtar **ve** etiketli girdi. Aynı anahtar altında bile invite MAC ≠
  session yapısı (ölçüldü); `config.Load` iki anahtarın eşitliğini reddediyor (gerçek arıza: tek
  `openssl rand` çıktısının iki yere yapıştırılması).
- **M5-01'in RED'i B fazında yeniden üretilmişti** (`activationState.code` çıplak `string` → `%+v` ham
  kodu bastı). `invite.Code`'a çevrildi. **Kalıp bir kez yazılınca bitmiyor, her yeni pakette tekrar
  düşülüyor.**

**redline R7 `code_hash`'i taramıyordu** → eklendi (kapsam genişlemesi). Sebep: `code_hash` bu tasarımda
**taşıyıcı (bearer) kimlik bilgisi** — hash tenant'ı çözer ve daveti harcar, yani loglanması kodu
loglamakla aynı. İlk desen *kelime* eşleştirip masum satırları (`"code expired"`) kırmızıya
döndürüyordu → **değer** hedefleyecek şekilde yeniden yazıldı: yanlış pozitif üreten kural gevşetilir,
gevşetilirken gerçek dal da gider.

**Toplam 7 aşırı iddia ölçümle çürütülüp indirildi.** Kalan sınırlar (fail-open `Sec-Fetch-Site`,
`ConfirmView.Code`'un tip korumasız oluşu, çerez gölgeleme, süreç-içi limiter) **sınır olarak** yazıldı.

**Kapsam dışı bırakılanlar (bilinçli):** davet üreten HTTP uç noktası **yok** (admin auth M6-01;
kimliksiz uç nokta Y-D'yi genişletirdi) · **§5 satır 3 bağlanmadı** (hedef var, `transactions` yazmıyor;
yönlendirme M5-04) · fontlar self-host değil (M5-04) · M5-03 middleware · M5-07 tur/practice.
**Sıradaki: M5-03** — gerçek IP devri adıyla yazıldı ("M5-03'e devralınan" md. 1).

### 2026-07-31 (5. oturum) — **M5-01 done · M5 başladı (1/10)**

`internal/session` teslim (`a71e1b2`): token üretimi/doğrulama/yenileme/iptal + çerez kodeği + 5
tenant-kapsamlı sqlc sorgusu. **Migration yazılmadı** — `sessions` 00003'te RLS beşlisi ve
`REVOKE DELETE` ile zaten tam; uygulanmış migration'a dokunulmadı (§6).

**Beş tur sürdü. İki RED ve ikisi de AYNI SINIFTAN — bu oturumun asıl çıktısı bu ders:**
> **Bir yorum "hiçbir çağıran X yapamaz / yapısal olarak imkânsız" diyorsa, X harici bir paketten
> DENENMİŞ olmalıdır. Denenmediyse iddia değil, *sınır* yazılır.**
1. **RED-1 (genel denetçi):** `Token` **unexported bir alanda** taşındığında `fmt`
   `Formatter/Stringer/GoStringer/LogValuer`'ı **atlıyor** (`CanInterface()==false`) ve `%v/%+v/%#v` +
   `slog` **ham token'ı** basıyordu — oysa `token.go` bunun "yapısal olarak imkânsız" olduğunu yazıyordu
   ve testi yalnız **exported** alanlı sarmalayıcıyı deniyordu (bu yüzden yeşildi ve hiçbir şey
   kanıtlamıyordu). Fix: `type Token struct{ v *string }` → dolaylılık adres bastırır. XOR-maske
   alternatifi §7 ("paket seviyesi singleton yok") gerekçesiyle reddedildi; `func`-alan comparability'yi
   bozardı. **Bedeli kaydedildi:** `==` artık **kimlik**, değer değil (sonuçları fail-closed).
2. **RED-2 (`tappa-security-auditor`):** `cookie.go` *"no caller can produce a non-Secure session cookie
   in production"* diyordu; ama **Go'da yasak olan alanı ADLANDIRMAKTIR, `T{}` yazmak değil** →
   `var c session.Cookies` (paketin **kendi** harici testinin kullandığı idiom!) `secure=false` verip
   `Set` **ve** `Clear`'da Secure'suz çerez yazıyordu. Fix: **kutup çevirme** → `struct{ insecure bool }`,
   sıfır değer **fail-closed**. Tehlikeli durum artık *kazara temsil edilemez*.

**Denetim derinliği (hepsi denetçilerin kendi komutlarıyla, yapıcının testlerinden bağımsız):**
`Verify`'ın **tek sorgu** oluşu gerçek Postgres'te **iki ayrı yolla** (`pg_stat_user_tables` deltası +
marker'lar arasına bracket'lenmiş `log_statement=all`) · RLS izolasyonu **üç denetçi tarafından ayrı ayrı**
`ALTER TABLE sessions DISABLE ROW LEVEL SECURITY` → test RED → geri açıp `pg_class` ile ölçerek doğrulama ·
sızıntı için **306 hücrelik matris + pozitif kontrol** (çıplak `string` aynı harness'ta 18 render'ın
13'ünde sızdı → sonda kör değil) · **17 sıfır-değer yolu** (embedded alan, kanal, `reflect.Zero`,
map-eksik-anahtar, `append` büyümesi…) × `Set`+`Clear` · **99 `Env`×`BaseURL`** kombinasyonu ·
sqlc çıktısının bağımsız yeniden üretilip **bayt bayt** diff'lenmesi (elle düzenleme yok).

**§5 satır 3 vs satır 4 — kartla gerçeğin çeliştiği yer, gerekçeli çözüldü.**
`resolve_session_by_token_hash` `employees.status` **döndürmüyor** (00003:189-205) → kartın "tek sorgu"
+ "deaktive anında geçersiz" ikilisi literal olarak sağlanamaz. Session katmanı **gerçeği taşır, karar
vermez**: `Verify` iptalde **dolu `Resolved` + `ErrRevoked`** döndürür ki çağıran §5 satır 4'ü (reject +
**KAYIT** + uyarı) uygulayabilsin. Aksi hâlde satır 4 satır 3'e (aktivasyon, **kayıt yok**) çökerdi ve
deneme iz bırakmadan kaybolurdu (§4.6). Guardrail sırası bunu doğruluyor: `sys:no-session` **#6**,
`sys:employee-deactivated` **#7**. Kart bir "Kart düzeltmesi" bloğuyla düzeltildi — kriter
**zayıflatılmadı, yeri değişti** ve düşürülen garantinin karşılığı üç maddeyle gösterildi.

**Kapsam genişlemesi (bilinçli, işaretli):** `TAPPA_ENV` kapalı küme `{dev,staging,prod}` oldu
(`internal/config`) — `NewCookies` artık `IsProd()` okuduğu için env bir **güvenlik niteliği**;
`TAPPA_ENV=production` insana doğru görünür ama `IsProd()`'u false yapar. Değiştirmeden önce mevcut tüm
değerler tarandı (`.env`, `.env.example`, Makefile, compose, CI → hepsi `dev` veya unset) — hiçbir şey kırılmadı.

**Ayrıca:** `DeviceInfo` sınırlandı (≤64 rune, geçerli UTF-8, Cc/Cf/Zl/Zp **ham dizede, trim'den ÖNCE**
reddedilir → konum önemsiz); **reddeder, kırpmaz** (sessiz kırpma = sessiz kabul, §7). Yapıcının ilk
taslağı 120'ydi ve **kendi testi dekoratif olduğunu yakaladı** (gerçek Chrome UA 117 rune, altından
geçiyordu).

**Kalan:** Q11 **AÇIK** (gerçek iPhone — "Bekleyen kullanıcı eylemi"). 7 devir notu →
"M5-02/M5-03'e devralınan"; en önemlisi **deaktif çalışanın çerezi canlı oturum olarak çözülmeye devam
eder** → tap dışındaki her kimlik yüzeyi `employees.status`'ü kendisi kontrol etmeli.
**Sıradaki: M5-02** (davet + aktivasyon; §5 satır 3 oraya bağlanıyor).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-07 done · 🏁 M4 KİLOMETRE TAŞI TAMAM (7/7)**

**M4-07 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R6/R8). `c5536be`: `table_test.go`
duplikasyon-ledger'ı ile §5 yedi satır + 5 zorunlu ek vaka. **Merkez:** debounce KİŞİ-bazlı vaka (farklı
kişiler aynı plaket 10sn→hepsi ok) — person-scoping'i nötrleyen mutasyon 3 satırı RED yaptı. R8: Evaluate tek
çağrı, erken-return yok (redline temiz). R6: row7-flag, no-session-tek-redirect, default→flag. isPracticeTap
sertleştirildi (+LastOpenIn==nil, kayıt yazımını etkilemez). %96.7 kapsam.

**🏁 M4 TAMAM (7/7):** internal/geo (haversine, DST-siz) · Input/Decision tipleri (saf) · Decide bağlam+delege
(§5 tablo motora, if-zinciri yok) · yön (son-açık-giriş toggle, gece vardiyası) · vardiya+geç kalma (DST doğru,
rapor-only) · trust/QR/practice (sunucu-türev, exploit kapalı) · tablo test (%96.7). Tüm motor saf
(time.Now/DB/HTTP yok, Now girdiden); policy motoru (M3) üstünde durur, karar taklidi yok.

**🔴 M5 için BLOKLAYAN + devirler (ŞU AN "M4/M5'e devralınan"da):** N5 tenant-mismatch (Input'a TagTenantID/
SessionTenantID, sys:tenant-mismatch besle — çapraz-tenant deliği), N1 tap:sunValid, N2 channel sunucu-türetimi,
N3 debounce→Params, N4 Decision→sütun sadakati, ErrUnknownTag logla, manuel entered_by (M5-05). Ayrıca düşük:
guardrails.go:222 yanıltıcı yorum → internal/policy sonraki dokunuş.

**Sırada:** M5-01 (internal/session — yeni kilometre taşı; M5 kartını baştan oku). **Milestone sınırı.**

### 2026-07-26 (4. oturum, compact sonrası) — **M4-06 done** (trust + QR + practice)

**M4-06 done — üçüncü göz 1. turda ONAY** (2 mutasyon öldürüldü). `a82dfa8`: Trust `20+50(IP)+30(GPS)`
verdict switch ÖNCESİ, **verdict'ten bağımsız** (reject 70 > ok 50 kanıtı). **Practice sunucu-türev**
(`ActivatedAt`+`LastForPerson==nil`); Input'ta client practice alanı YOK (reflection guard denetçi tarafından
Input'a alan eklenerek RED kanıtlandı); checkout asla practice → **saat-şişirme exploit'i yapısal kapalı**.
QR uçtan uca policy'de (base:qr-requires-ip): QR+IP-yok+GPS-var→flag (Q15, GPS tek başına kurtarmaz), QR+IP→ok,
SUN-suz QR sys:sun-invalid'e takılmaz. Manuel SUN atlar; entered_by write-path → M5-05 kartına kriter eklendi
(Decide saf func hata dönemez). %96.7 kapsam.

**Bloklamayan sertleştirme (→M4-07):** `isPracticeTap` yalnız LastForPerson==nil'e bakıyor; tutarsız çağıran
(LastOpenIn!=nil, LastForPerson==nil) checkout'u practice yapabilir — **client-erişilemez, tutarlı M5 sorgusundan
doğamaz**; opsiyonel: isPracticeTap'e `LastOpenIn==nil` ekle (resolveDirection'ın stale-practice guard'ını yansıtır).

**Sırada:** M4-07 (tablo bazlı test seti — §5 yedi satır + zorunlu ek vakalar [debounce KİŞİ-bazlı!], kapsam %90+,
iki denetçi R6/R8). **M4'ün son görevi.**

### 2026-07-26 (4. oturum, compact sonrası) — **M4-05 done** (vardiya çözümü + geç kalma, DST)

**M4-05 done — üçüncü göz 1. turda ONAY.** `63f6b4a`: `Decide` geç kalmayı `Input.Shift`+Now+`time.LoadLocation
("Europe/Malta")` ile hesaplar (departman/lokasyon çözümü çağıranın — M4-02 sözleşmesi). **DST denetçi tarafından
BAĞIMSIZ yeniden hesaplandı** (Python zoneinfo): mart 09:15→15, ekim 09:20→20, overnight 01:00→420; naif
midnight+offset bug'ı (−45/80) mutasyonla yakalandı. tzdata tap paketine gömülü (tek binary scratch image).

**Geç kalma RAPOR-only:** `Decision.MinutesLate *int` (nil=hesaplanmadı; **int dakika, float YOK** §6); Evaluate
SONRASI hesaplanır, context'e GİRMEZ, hiçbir baseline/guardrail `time:minutesLate` okumaz → **verdict'i etkilemez**
(180-geç→OK). Yalnız check-IN'de (checkout asla geç). Çapraz-lokasyon Q17: `employee:crossLocation`→base:cross-
location-note + `Decision.CrossLocation` (geç damgası yok). Shift==nil VE boş-tz→nil (LoadLocation("")→UTC tuzağı
guard'lı). %96.4 kapsam. cmd/tappa dokunulmadı.

**Sırada:** M4-06 (trust 20+50+30, QR base:qr-requires-ip, practice sunucu-türetimi saat-şişirme exploit'i).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-04 done** (yön tayini in/out)

**M4-04 done — üçüncü göz 1. turda ONAY** (4 mutasyon öldürüldü). `703d3d1`: `Decide` `Decision.Type`'ı
`Input.LastOpenIn`'e göre saf toggle (açık giriş→out, yok→in). **Takvim-günü filtresi YOK** — denetçi
bağımsız cross-midnight/ay/yıl/artık-gün testleriyle + 5h farkın 400 gün boyunca sabit kaldığıyla kanıtladı
(gece vardiyası bug'ının kaynağı bu filtredir). Stale open-in **>18h** → out + note (asla sessiz in; strict >).
Practice LastOpenIn → in muamelesi (eğitim tap'i gerçek check-in'i açık tutamaz — M4-06 saat-şişirme exploit'i;
asıl dışlama M5 sorgusunda). Type yalnız ok/flag'te (reject/ignored/redirect→nil). UTC saf süre, sabit-Now.

**Sırada:** M4-05 (vardiya çözümü + geç kalma — departman>lokasyon, çapraz-lokasyon Q17, DST Malta, geç kalma
verdict'i etkilemez, float değil).

### 2026-07-26 (4. oturum, compact sonrası) — **M4-03 done** (Decide: bağlam kurma + kararın uygulanması)

**M4-03 done — iki denetçi ONAY** (üçüncü göz + tappa-security-auditor R8; §4.6/§5, kırmızı çizgi ihlali yok).
`bfbbf77`: `tap.Decide` gövdesi — Input→policy.Context (ipMatch/gpsMatch/gpsDistanceM/gpsConflict/ctrGap +
guardrail alanları) → `policy.Evaluate` **tek çağrı, if-zinciri/erken-return YOK** → effect→verdict. no-session→
redirect+kayıt-yok (§5.3 tek istisna); row7→flag; boş set→flag (§4.6 sessiz-ok yok). %95.7 kapsam.

**R8 mutasyonla + kod-okumasıyla:** deactivated+invalid-SUN → sun-invalid kazanır + **Security=false** (deaktivasyon
sızmaz, push seli yok); üçüncü göz erken-return ekleyip RED gördü. **Marker-hilesi iki yönlü doğrulandı:**
SessionTenantID=Employee!=nil işareti (sys:no-session sürücüsü); TagTenantID nil → sys:tenant-mismatch ilk-clause
kısa-devre, gerçekten inert (misfire yok). PolicySet Input alanı olarak eklendi (imza korundu); Decision'a
MatchedSid/Layer/PolicyVersionID (M3-07). M4-02 kartı düzeltildi. Type/Trust/lateness/Practice M4-04/05/06'da.

**🔴 M5 için BLOKLAYAN devir (N5, ŞU AN'a yazıldı):** tag çözümü context-less (ADR 0002 md.7) → RLS çapraz-tenant
tag'i çözümde gizlemez. Decide tenant-farkındalıksız (Input'ta tenant id yok) → çapraz-tenant tap `sys:tenant-mismatch`
beslenmezse `ok` yazılır (izolasyon deliği). M4-03 doğru şekilde erteledi (karar taklidi yapmıyor); **M5 Input'u
TagTenantID/SessionTenantID ile genişletip guardrail'i ateşlemek ZORUNDA — tek gerçek engel.**

**Sırada:** M4-04 (yön tayini — son açık girişe göre toggle, gece vardiyası, practice zincire girmez).

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

🔴 **[2026-08-19, M2-08'DE GERİ ALINDI — bu paragrafın "yapısal düzeltme" ve "kanıtlandı"
cümleleri YANLIŞ.]** Teşhis doğruydu (sayaç tersti), **düzeltmenin yönü yanlıştı**. NXP AN12196
rev. 1.8 s.10, aynı UID için aynı sayfada iki şeyi birden yayımlıyor: §4.3 tablo 2 adım 4
`SDMReadCtr = 010000 (LSB first)` ve §4.4.1 URL'sinde `ctr=000001` — yani URL metni ile SV2
girdisi **kasten ters**. Verbatim taşımak, gerçek bir çipin sayacı palindrom olmayan **her**
tap'ini reddederdi (3 baytta palindrom 1/256; **1..255 aralığında hiç yok**, yani ilk 255 tap'in
hepsi). *"Bağımsız Python CMAC"* bunu göremezdi çünkü Python **kodun kendi sırasını** yeniden
uyguluyordu; iç-tutarlı bir zincirin ikinci kopyası bağımsız kanıt değildir. Ve sevk edilen test
düzeltmeyi yalnız kaçırmadı — **adıyla yasakladı**. M2-08 `sv2()`'yi ters çevirdi ve belgeden
transkribe edilmiş bir dış KAT ekledi; kusur üretimde hiç tetiklenmedi çünkü encode aracı hiç
yazılmadığı için **hiç plaket doğmadı**.

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
