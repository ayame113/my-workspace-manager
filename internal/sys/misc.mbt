///|
let base36_digits = "0123456789abcdefghijklmnopqrstuvwxyz"

///|
/// プロセス間・呼び出し間で衝突しにくい名前の部品 `<pid>-<ミリ秒の36進>-<乱数>`。
pub fn unique_suffix() -> String {
  let random = StringBuilder()
  let bytes = @env.rand(6).unwrap_or(b"\x00\x00\x00\x00\x00\x00")
  for byte in bytes {
    random.write_char(base36_digits.get_char(byte.to_int() % 36).unwrap())
  }
  "\{process_id()}-\{@env.now().to_string(radix=36)}-\{random.to_string()}"
}

///|
/// 現在時刻を JavaScript の `Date.prototype.toISOString` と同じ形式(`2026-01-02T03:04:05.678Z`)で返す。
pub fn iso_timestamp() -> String {
  format_iso_timestamp(@env.now().reinterpret_as_int64())
}

///|
pub fn format_iso_timestamp(epoch_milliseconds : Int64) -> String {
  let milliseconds_per_day = 86400000L
  let days = epoch_milliseconds / milliseconds_per_day
  let in_day = epoch_milliseconds % milliseconds_per_day
  // civil_from_days アルゴリズム(エポックからの日数 → 年月日)
  let z = days + 719468L
  let era = z / 146097L
  let day_of_era = z - era * 146097L
  let year_of_era = (
      day_of_era -
      day_of_era / 1460L +
      day_of_era / 36524L -
      day_of_era / 146096L
    ) /
    365L
  let day_of_year = day_of_era -
    (365L * year_of_era + year_of_era / 4L - year_of_era / 100L)
  let month_index = (5L * day_of_year + 2L) / 153L
  let day = day_of_year - (153L * month_index + 2L) / 5L + 1L
  let month = if month_index < 10L {
    month_index + 3L
  } else {
    month_index - 9L
  }
  let year = year_of_era + era * 400L + (if month <= 2L { 1L } else { 0L })
  let pad = (value : Int64, width : Int32) => {
    value.to_string().pad_start(width, '0')
  }
  "\{pad(year, 4)}-\{pad(month, 2)}-\{pad(day, 2)}T\{pad(in_day / 3600000L, 2)}:\{pad(in_day / 60000L % 60L, 2)}:\{pad(in_day / 1000L % 60L, 2)}.\{pad(in_day % 1000L, 3)}Z"
}
