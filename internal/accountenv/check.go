// Package accountenv 校验账户语言、时区与出口 IP 所在地区的一致性
package accountenv

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Warning 表示一处环境不一致，Code 供前端识别，Message 面向用户
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Suggestion 表示按地区推导出的建议语言与时区，空字段表示没有建议
type Suggestion struct {
	Locale   string `json:"locale,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// countryLanguages 是各国家或地区的主要语言，用于建议语言
var countryLanguages = map[string]string{
	"AE": "ar", "AR": "es", "AT": "de", "AU": "en", "BE": "nl", "BG": "bg",
	"BR": "pt", "CA": "en", "CH": "de", "CL": "es", "CN": "zh", "CO": "es",
	"CZ": "cs", "DE": "de", "DK": "da", "EE": "et", "EG": "ar", "ES": "es",
	"FI": "fi", "FR": "fr", "GB": "en", "GR": "el", "HK": "zh", "HR": "hr",
	"HU": "hu", "ID": "id", "IE": "en", "IL": "he", "IN": "en", "IS": "is",
	"IT": "it", "JP": "ja", "KE": "en", "KR": "ko", "LT": "lt", "LU": "fr",
	"LV": "lv", "MA": "ar", "MX": "es", "MY": "ms", "NL": "nl", "NO": "no",
	"NZ": "en", "PE": "es", "PH": "en", "PL": "pl", "PT": "pt", "RO": "ro",
	"RS": "sr", "RU": "ru", "SA": "ar", "SE": "sv", "SG": "en", "SI": "sl",
	"SK": "sk", "TH": "th", "TR": "tr", "TW": "zh", "UA": "uk", "US": "en",
	"VN": "vi", "ZA": "en",
}

// Check 校验语言与时区的自洽性，返回警告与建议；不涉及网络
func Check(locale, timezone string) ([]Warning, Suggestion) {
	warnings := make([]Warning, 0, 2)
	var suggestion Suggestion
	language, region, localeValid := SplitLocale(locale)
	if !localeValid {
		warnings = append(warnings, Warning{Code: "invalid_locale", Message: "语言格式无效，应写成 语言-地区，例如 en-US"})
	} else if region == "" {
		warnings = append(warnings, Warning{Code: "locale_missing_region", Message: "语言缺少地区部分，例如应为 en-US 而不是 en"})
	}
	zone := strings.TrimSpace(timezone)
	countries, zoneValid := ZoneCountries(zone)
	if !zoneValid {
		warnings = append(warnings, Warning{Code: "invalid_timezone", Message: "时区不是合法的 IANA 名称，例如 Asia/Shanghai 或 America/Los_Angeles"})
	}
	if zoneValid && region != "" && len(countries) > 0 && !slices.Contains(countries, region) {
		warnings = append(warnings, Warning{
			Code:    "locale_timezone_mismatch",
			Message: fmt.Sprintf("语言地区 %s 与时区 %s（%s）不一致", region, zone, strings.Join(countries, "/")),
		})
		if representative, ok := countryZones[region]; ok {
			suggestion.Timezone = representative
		}
		if len(countries) == 1 {
			suggestion.Locale = suggestLocale(language, countries[0])
		}
	}
	if !zoneValid && region != "" {
		if representative, ok := countryZones[region]; ok {
			suggestion.Timezone = representative
		}
	}
	if localeValid && region == "" && zoneValid && len(countries) == 1 {
		suggestion.Locale = language + "-" + countries[0]
	}
	return warnings, suggestion
}

// CheckExit 在 Check 的基础上比对出口 IP 所在国家，返回警告与建议
func CheckExit(locale, timezone, country string) ([]Warning, Suggestion) {
	warnings, suggestion := Check(locale, timezone)
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return warnings, suggestion
	}
	language, region, localeValid := SplitLocale(locale)
	if localeValid && region != "" && region != country {
		warnings = append(warnings, Warning{
			Code:    "locale_exit_mismatch",
			Message: fmt.Sprintf("出口 IP 位于 %s，与语言地区 %s 不一致", country, region),
		})
	}
	if countries, ok := ZoneCountries(timezone); ok && len(countries) > 0 && !slices.Contains(countries, country) {
		warnings = append(warnings, Warning{
			Code:    "timezone_exit_mismatch",
			Message: fmt.Sprintf("出口 IP 位于 %s，与时区 %s（%s）不一致", country, strings.TrimSpace(timezone), strings.Join(countries, "/")),
		})
	}
	if representative, ok := countryZones[country]; ok {
		suggestion.Timezone = representative
	}
	suggestion.Locale = suggestLocale(language, country)
	return warnings, suggestion
}

// SuggestForCountry 返回国家或地区的建议语言与时区，未收录时返回空
func SuggestForCountry(country string) (locale, timezone string) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return "", ""
	}
	return suggestLocale("", country), countryZones[country]
}

// SplitLocale 拆分语言与地区，语言小写、地区大写；地区可省略，格式无效时 valid 为 false
func SplitLocale(locale string) (language, region string, valid bool) {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "", "", false
	}
	language, region, found := strings.Cut(strings.ReplaceAll(locale, "_", "-"), "-")
	language = strings.ToLower(language)
	if len(language) < 2 || len(language) > 3 || !letters(language) {
		return "", "", false
	}
	if !found {
		return language, "", true
	}
	region = strings.ToUpper(region)
	if len(region) == 2 && letters(region) || len(region) == 3 && digits(region) {
		return language, region, true
	}
	return "", "", false
}

// ZoneCountries 返回时区使用的国家或地区集合，时区非法时 valid 为 false；UTC 等无国家时区返回空集合
func ZoneCountries(timezone string) ([]string, bool) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return nil, false
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, false
	}
	return zoneCountries[location.String()], true
}

// suggestLocale 按国家的主要语言给出语言-地区，主要语言缺失时沿用当前语言
func suggestLocale(language, country string) string {
	primary := countryLanguages[country]
	if primary == "" || primary == language {
		if language == "" {
			primary = "en"
		} else {
			primary = language
		}
	}
	return primary + "-" + country
}

// letters 表示全部为英文字母
func letters(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return value != ""
}

// digits 表示全部为数字
func digits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
