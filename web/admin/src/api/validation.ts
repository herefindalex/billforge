const utcTimestamp = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$/
const minUnixNanoKey = '16770921001243145224192'
const maxUnixNanoKey = '22620411234716854775807'

// The server stores business timestamps as int64 Unix nanoseconds. Compare
// fixed-width decimal parts so no JS Date or floating-point conversion loses
// fractional precision near the storage boundaries.
export function isExactAdminUTC(value: string): boolean {
  const parts = utcTimestamp.exec(value)
  if (!parts) return false
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction = ''] = parts
  const year = Number(yearText)
  const month = Number(monthText)
  const day = Number(dayText)
  const hour = Number(hourText)
  const minute = Number(minuteText)
  const second = Number(secondText)
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return false
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (day < 1 || day > days[month - 1]) return false
  const key = `${yearText}${monthText}${dayText}${hourText}${minuteText}${secondText}${fraction.padEnd(9, '0')}`
  return key >= minUnixNanoKey && key <= maxUnixNanoKey
}

export function isNonNegativeInt64String(value: string): boolean {
  if (!/^\d+$/.test(value)) return false
  const digits = value.replace(/^0+/, '') || '0'
  const maximum = '9223372036854775807'
  return digits.length < maximum.length || (digits.length === maximum.length && digits <= maximum)
}

export function minimumUpfrontFits(fixedMinor: string, seatMinor: string): boolean {
  if (!isNonNegativeInt64String(fixedMinor) || !isNonNegativeInt64String(seatMinor)) return false
  return BigInt(fixedMinor) > 0n && BigInt(fixedMinor) + BigInt(seatMinor) <= 9223372036854775807n
}
