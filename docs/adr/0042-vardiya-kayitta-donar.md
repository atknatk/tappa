# ADR 0042 — Dokunuşun ölçüldüğü vardiya kayıtta donar (`transactions.shift_*`)

- **Durum:** Önerildi (2026-11-09), denetim bekliyor.
- **Tarih:** 2026-11-09 · **Backlog:** T152 · **Dal:** `m14-shiftsnap` · **Migration:** 00095
- **Karar sahibi:** kullanıcı, 2026-11-08: *"tap anında geçerli vardiyayı kayda yaz; eski satırlar bugünkü davranışı korusun ve rapor bunu söylesin"*.
- **İlgili:** CLAUDE.md §4.3 (kayıt değişmez), §5 (geç kalma: departman vardiyası, yoksa **dokunulan** lokasyonun vardiyası), §6 · ADR 0038 (sütun düzeyi INSERT yetkisi) · ADR 0032 §A.5 (`business_day_start` aynı gerekçeyle donar) · ADR 0006 · Q17
- **Ölçüldü** = bu dalda, şerit veritabanı `tappa-m14-shiftsnap-db` (goose 94→95). **Okundu** = dosyadan, çalıştırılmadı.

## Bağlam

Geç kalma **okuma anında** hesaplanıyordu. `ListWorkedShiftEvents`, `locations` ve `departments` satırlarını bugünkü hâlleriyle join'liyordu; `ledger.resolveShift` vardiyayı oradan alıyor, `tap.MinutesLate(occurred_at, vardiya)` dakikayı veriyordu (okundu). Bir mekânın ya da departmanın saati değişince geçmiş haftaların geç kalma sayıları da değişiyordu. Kayıt değişmiyordu ama rapor değişiyordu. Bu `business_day_start`'ın (00064) çözdüğü sorunun aynısıdır: kesim saati sonradan değişebilir, bugünkü ayardan geçmişi yeniden hesaplayan okuyucu geçen haftayı değiştirir.

Aynı zamanda `tap.Decision.MinutesLate` motorda hesaplanıyor ama hiçbir ürün kodunda okunmuyordu (okundu: tek okuyucu testler). Gather'ın vardiya okuması yalnız onu besliyordu.

## Karar

1. **Beş sütun, hepsi nullable, default yok, geri doldurma yok** (§4.3): `shift_source` (`'department' | 'location' | 'none'`), `shift_start`, `shift_end` (`time`, 00002'nin kodlaması), `shift_overnight` (`boolean`), `shift_timezone` (`text`, IANA). Duvar saati, saat dilimi olmadan bir an değildir. Tenant'ın dilimi de düzenlenebilir (00024), bu yüzden dilim de donar.
2. **Yazan yalnız dokunuşun INSERT'idir** (`InsertTransaction`, `checkin.insertParams` → `shiftColumns`). Yazılan değer motora verilen `Input.Shift`'in kendisidir: departmanın vardiyası varsa o, yoksa dokunulan lokasyonun (holding eşinin plaketinde eşin mekânı) — §5 ve Q17 harfiyen. Kaynak `department` ya da `location` olarak yazılır. Vardiya çözülmediyse `'none'` yazılır: bu "vardiyasız ölçüldü" demektir ve kayda geçen bir cevaptır. Rapor bu satırı sonradan kurulan bir vardiyayla **yeniden ölçmez**.
3. **Yalnız yönlü nfc/qr kararında** (business_day_start ve direction_source'un kuralı). Undirected kararda (reject, ignored) hepsi NULL kalır. **Manual satırda da hepsi NULL kalır.** Gerekçe: müdürün satırı, vardiyanın son düzenlenmesinden **önceki** bir `occurred_at` beyan edebilir. Yazıldığı andaki vardiya, olduğu andaki vardiyanın kanıtı değildir. Onu dondurmak bir tahmini kayıt gibi saklamak olurdu. Üç manual ifade sütunları anmaz; daraltma ifadenin kendisidir (M6-08). Manual varışlar raporda zaten ayrı sayılır (`ManualArrivals`).
4. **Rapor:** `shift_source` doluysa vardiya kayıttandır (`ledger.recordedShift`). Kayıtlı dilim kullanılır, bugünkü tenant dilimi değil. NULL ise bugünkü ayardan çözülür (`resolveShift`, eski davranış) ve varış `Totals.ShiftFromToday`'e sayılır. Ekran bunu bir cümleyle söyler (aşağıda). CSV aynı sayıyı "What is NOT in the hours below" bloğunda bir satır olarak taşır.
5. **`Decision.MinutesLate` kalır, saklanmaz.** Kayıt **sayıyı** değil, sayının hesaplandığı **vardiyayı** taşır. Rapor dakikayı aynı tek fonksiyonla (`tap.MinutesLate`) anlıktan yeniden hesaplar. Ayrı bir `minutes_late` sütunu, satırın zaten belirlediği değerin ikinci kopyası olurdu. Alanı silmek `TestDecide_*` testlerini değiştirirdi; bu kart §5 tablosuna ve motorun kararına dokunmaz. Alan artık motorun, kaydın yeniden üretmek zorunda olduğu cevabıdır: checkin DB testleri her check-in için `tap.MinutesLate(occurred_at, kayıttaki vardiya) == Decision.MinutesLate` olduğunu doğrular.

### Ekrandaki cümle (birebir)

- `Late minutes for 1 arrival use the current shift times — it was typed by a manager, or recorded before Taptime saved the shift with each record.`
- `Late minutes for N arrivals use the current shift times — they were typed by a manager, or recorded before Taptime saved the shift with each record.`

Cümle yalnız sayı > 0 iken görünür. CSV satırı: `Arrivals measured against the current shift` + sayı + not.

### Şema ayrıntısı

- **CHECK `transactions_shift_snapshot_shape`** üç biçime izin verir: hepsi NULL · `'none'` + dört değer NULL · `department`/`location` + dört değer dolu. Son ikisi ayrıca yönlü ve nfc/qr satır ister. Bu ikinci emniyettir: biçimi `shiftColumns` kodda zorlar. CHECK'e takılan bir kod hatası 23514 ile kaydı kaybettirir (§4.6), bu yüzden fonksiyon tanımadığı bir kaynağı hiç yazmaz.
- **NULL deliği (ölçüldü):** CHECK ilk yazıldığında, kaynağı NULL olup değerleri dolu bir satır son kolu `false` değil `NULL` yapıyordu. NULL dönen bir CHECK satırı kabul eder. `TestShiftSnapshot00095_TheCheckAdmitsTheTapsThreeShapesAndRefusesEveryOther`'ın "values with no source" vakası satırı kabul ettirdi. Kaynak adlandıran her kol artık önce `shift_source IS NOT NULL` diyor; vaka 23514 veriyor.
- **NOT VALID kalır**, VALIDATE dosyası yok. Default'suz ADD COLUMN her eski satırı NULL bırakır ve ilk kol bunu kabul eder. VALIDATE en sıcak tabloyu baştan sona okuyup ALTER'ın zaten garanti ettiğini kanıtlardı. NOT VALID yeni satırlarda yine uygulanır.
- **Kilit:** ALTER yalnız kataloğa dokunur. 50 000 satırlık kopya üzerinde `seq_scan` farkı 0 ölçüldü. Kontrol olarak NOT VALID'siz aynı ALTER tabloyu taradı (`TestShiftSnapshot00095_TheAlterScansNothing`). İlk ifade `lock_timeout = '1s'`'dir (sıcak tablo kuralı).
- **Yetki (ADR 0038'in kuralı):** `GRANT INSERT (shift_source, shift_start, shift_end, shift_overnight, shift_timezone) ON transactions TO tappa_app` aynı dosyada verilir. DO bloğu sonucu 00088 gibi *"id ve created_at dışındaki her sütun"*a tutar. UPDATE yok (§4.3). Başka hiçbir rol yeni sütunlara erişmez (feed yazıcılarının yetkileri sütun listesidir — `m11Columns`, `privCatalogAllowed` güncellendi).
- **Down:** önce `LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE`, sonra bakar. Bir satır snapshot taşıyorsa 55000 ile reddeder ve hiçbir şeyi değiştirmez (00043/00064/00065 emsali: kanıt düşürülmez). Snapshot yoksa sütunları ve CHECK'i düşürür; sütun yetkileri sütunla gider. İlk dokunuştan sonra geri dönüş şemayla değil binary ile olur: eski binary sütunları ne yazar ne okur.

## Eski binary uyumu (expand/contract)

Migrate Job rollout'tan önce, eski pod servis verirken koşar. Eski `InsertTransaction` beş sütundan hiçbirini anmaz, yetkili olduğu 30 sütunu yazar, yeni sütunlar NULL kalır. NULL eski raporun her satıra verdiği cevaptır. Eski SELECT'ler sütun adlandırır (`db/queries`'te `SELECT *` yok — okundu), dolayısıyla daha geniş bir satır hiçbir taramayı bozmaz. Ölçüldü: önceki binary'nin ifade metni tappa_app olarak RLS altında 00095 şemasında yazıldı, 32 sütun döndürdü, snapshot NULL kaldı (`TestShiftSnapshot00095_TheOldBinarysInsertStillLands`). Yeni binary eski şemada koşmaz (migrate önce).

## Değerlendirilen seçenekler

| Seçenek | Neden |
|---|---|
| **(a) Vardiyayı kayda dondur** | **Seçildi** (kullanıcı kararı). Rapor tek fonksiyondan hesaplar; dakika ikinci kez saklanmaz. |
| (b) `minutes_late integer` sütunu | Satırın belirlediği bir sayının ikinci kopyası. Hesap düzeltilirse (M6-07'nin −520 hatası gibi) saklı sayı yanlış kalır ve §4.3 onu düzeltmeyi yasaklar. Ayrıca "vardiya yoktu" ile "00095'ten önce" ayrımını tek NULL'da kaybeder. |
| (c) Vardiya geçmişi tablosu (geçerlilik aralıklı) | Her yazıcıda (panel, API) tetikleyici ya da ikinci yazım ister, daha büyük bir iştir. Raporun her satır için zamanı aralıkla eşlemesi gerekir. Manual satırın beyan ettiği ana da cevap verirdi ama bu kart onu istemiyor (aşağıda "Açık kalan"). |
| (d) Yalnız raporda uyarı | Zararı kapatmaz: geçmiş haftalar yine değişir. Eski satırlar için bu ADR'nin cümlesi zaten bunu yapar. |

## Kırmızı çizgi analizi

§4.3: UPDATE yok, geri doldurma yok. Yeni sütunlar yalnız INSERT anında yazılır, `transactions_no_mutation` tetikleyicisi değişmedi. §4.5: yeni sorgu yok. Değişen dört ifadenin hepsi açık `tenant_id` koşulunu korur ve RLS değişmedi. §4.6: CHECK ikinci emniyettir, ilk emniyet kodda biçimi zorlamaktır. §4.7: yeni sütunlar kanıt değildir (bir kelime, iki saat, bir bool, bir dilim adı), konum ya da anahtar taşımaz.

## Açık kalan

- **Manual satırlar** bugünkü vardiyayla ölçülmeye devam eder ve ekran bunu söyler. Manual satırın beyan ettiği andaki vardiyayı bilmek için bir vardiya geçmişi gerekir (seçenek c). Ayrı bir ürün kararıdır.
- **Sonradan Down.** İlk dokunuştan sonra 00095'in Down'ı reddeder (yukarıda). Bu bilinçlidir.
