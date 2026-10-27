package agent

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Time-zone rules are available even in a minimal runtime image.
)

// Exact, context-free requests only. Unknown clauses and business operations
// must reach the normal agent with their original text and documents intact.
var utilityClock = regexp.MustCompile(`^(?:la )?(que hora es(?: ahora)?|que hora hay|hora actual|hora|que fecha es|que fecha es hoy|que dia es hoy|que dia de la semana es hoy|fecha actual|fecha de hoy|fecha|que hora y fecha es|que fecha y hora es)(?: (?:en|de) (.+))?$`)
var utilityArithmetic = regexp.MustCompile(`^(?:(?:cuanto es|cuanto da|calcula) )?(-?[0-9]{1,12}(?:[.,][0-9]{1,8})?)\s*([+*/×÷-])\s*(-?[0-9]{1,12}(?:[.,][0-9]{1,8})?)$`)
var utilityPercent = regexp.MustCompile(`^(?:(?:cuanto es|calcula) )?(?:el )?([0-9]{1,12}(?:[.,][0-9]{1,8})?)\s*% de (-?[0-9]{1,12}(?:[.,][0-9]{1,8})?)$`)
var utilityConversion = regexp.MustCompile(`^(?:(?:convierte|convertir) )?(-?[0-9]{1,12}(?:[.,][0-9]{1,8})?) (mm|cm|m|km|milimetros|centimetros|metros|kilometros|mg|g|kg|t|miligramos|gramos|kilogramos|toneladas|ml|l|mililitros|litros|segundos|minutos|horas|dias) (?:a|en) (mm|cm|m|km|milimetros|centimetros|metros|kilometros|mg|g|kg|t|miligramos|gramos|kilogramos|toneladas|ml|l|mililitros|litros|segundos|minutos|horas|dias)$`)

var utilityZones = map[string]string{
	"colombia": "America/Bogota", "bogota": "America/Bogota", "medellin": "America/Bogota", "cali": "America/Bogota",
	"peru": "America/Lima", "lima": "America/Lima", "ecuador continental": "America/Guayaquil", "quito": "America/Guayaquil",
	"argentina": "America/Argentina/Buenos_Aires", "buenos aires": "America/Argentina/Buenos_Aires",
	"uruguay": "America/Montevideo", "montevideo": "America/Montevideo", "venezuela": "America/Caracas", "caracas": "America/Caracas",
	"panama": "America/Panama", "costa rica": "America/Costa_Rica", "guatemala": "America/Guatemala",
	"madrid": "Europe/Madrid", "barcelona": "Europe/Madrid", "valencia": "Europe/Madrid", "bilbao": "Europe/Madrid", "sevilla": "Europe/Madrid",
	"canarias": "Atlantic/Canary", "tenerife": "Atlantic/Canary", "londres": "Europe/London", "paris": "Europe/Paris",
	"berlin": "Europe/Berlin", "roma": "Europe/Rome", "lisboa": "Europe/Lisbon", "tokio": "Asia/Tokyo", "japon": "Asia/Tokyo",
	"china": "Asia/Shanghai", "pekin": "Asia/Shanghai", "india": "Asia/Kolkata", "dubai": "Asia/Dubai",
	"nueva york": "America/New_York", "miami": "America/New_York", "los angeles": "America/Los_Angeles",
	"ciudad de mexico": "America/Mexico_City", "santiago de chile": "America/Santiago", "sao paulo": "America/Sao_Paulo",
	"utc": "UTC", "gmt": "UTC",
}

func odooUtilityReply(text, userZone string, now time.Time) (string, bool) {
	if len(text) > 300 {
		return "", false
	}
	q := strings.Trim(normalizeOdooQuestion(text), " ?!.")
	q = trimOdooQuestionPoliteness(q)
	for _, greeting := range []string{"hola, ", "hola "} {
		if strings.HasPrefix(q, greeting) {
			q = strings.TrimSpace(strings.TrimPrefix(q, greeting))
			break
		}
	}
	switch q {
	case "gracias", "muchas gracias", "muchisimas gracias", "ok gracias", "vale gracias":
		return "¡De nada!", true
	case "ayuda", "como uso el chat", "como uso odooclaw":
		return "Escribe tu consulta o tarea de Odoo. Puedes adjuntar archivos y usar «Añadir registro actual» desde un formulario guardado. Las acciones respetan los permisos de tu usuario. «Mi sesión» y «Modelo y ajustes» muestran datos del chat sin IA cuando están disponibles.", true
	}
	if m := utilityClock.FindStringSubmatch(q); m != nil {
		zone, label := userZone, "tu zona horaria de Odoo"
		if m[2] != "" {
			var ok bool
			zone, ok = utilityZones[m[2]]
			if !ok {
				// Multi-zone countries require a city; never invent a single hour.
				switch m[2] {
				case "españa", "estados unidos", "mexico", "brasil", "canada", "australia", "rusia", "chile", "ecuador", "portugal":
					return "Ese país tiene varias zonas horarias. Indica la ciudad para darte la hora o fecha exacta.", true
				}
				return "", false
			}
			label = m[2]
		}
		if zone == "" {
			return "Indica la ciudad o zona horaria, por ejemplo: «¿Qué hora es en Colombia?».", true
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return "", false
		}
		local := now.In(loc)
		zoneName, offset := local.Zone()
		sign := "+"
		if offset < 0 {
			sign = "-"
			offset = -offset
		}
		suffix := fmt.Sprintf("%s · %s · UTC%s%02d:%02d", label, zoneName, sign, offset/3600, offset%3600/60)
		clock, date := strings.Contains(m[1], "hora"), strings.Contains(m[1], "fecha") || strings.Contains(m[1], "dia")
		if clock && date {
			return fmt.Sprintf("Fecha y hora actuales: **%s · %s** (%s).", local.Format("02/01/2006"), local.Format("15:04:05"), suffix), true
		}
		if clock {
			return fmt.Sprintf("Hora actual: **%s** (%s).", local.Format("15:04:05"), suffix), true
		}
		days := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
		return fmt.Sprintf("Hoy es **%s, %s** (%s).", days[local.Weekday()], local.Format("02/01/2006"), suffix), true
	}
	if m := utilityArithmetic.FindStringSubmatch(q); m != nil {
		a, b := utilityNumber(m[1]), utilityNumber(m[3])
		result := new(big.Rat)
		switch m[2] {
		case "+":
			result.Add(a, b)
		case "-":
			result.Sub(a, b)
		case "*", "×":
			result.Mul(a, b)
		case "/", "÷":
			if b.Sign() == 0 {
				return "No se puede dividir entre cero.", true
			}
			result.Quo(a, b)
		}
		return fmt.Sprintf("%s %s %s = **%s**", m[1], m[2], m[3], utilityResult(result)), true
	}
	if m := utilityPercent.FindStringSubmatch(q); m != nil {
		result := new(big.Rat).Mul(utilityNumber(m[1]), utilityNumber(m[2]))
		result.Quo(result, big.NewRat(100, 1))
		return fmt.Sprintf("El %s %% de %s es **%s**.", m[1], m[2], utilityResult(result)), true
	}
	if m := utilityConversion.FindStringSubmatch(q); m != nil {
		from, to := utilityUnits[m[2]], utilityUnits[m[3]]
		if from.dimension != to.dimension {
			return "", false
		}
		result := new(big.Rat).Mul(utilityNumber(m[1]), big.NewRat(from.scale, to.scale))
		return fmt.Sprintf("%s %s = **%s %s**.", m[1], m[2], utilityResult(result), m[3]), true
	}
	return "", false
}

func utilityNumber(s string) *big.Rat {
	n, _ := new(big.Rat).SetString(strings.ReplaceAll(s, ",", "."))
	return n
}
func utilityResult(n *big.Rat) string {
	if n.IsInt() {
		return n.Num().String()
	}
	// Exact rational arithmetic, finite display rounded to eight decimals.
	value := strings.TrimRight(strings.TrimRight(n.FloatString(8), "0"), ".")
	displayed := utilityNumber(value)
	if displayed.Cmp(n) != 0 {
		value = "≈ " + value
	}
	return strings.ReplaceAll(value, ".", ",")
}

type utilityUnit struct {
	dimension string
	scale     int64
}

var utilityUnits = map[string]utilityUnit{
	"mm": {"length", 1}, "milimetros": {"length", 1}, "cm": {"length", 10}, "centimetros": {"length", 10}, "m": {"length", 1000}, "metros": {"length", 1000}, "km": {"length", 1000000}, "kilometros": {"length", 1000000},
	"mg": {"mass", 1}, "miligramos": {"mass", 1}, "g": {"mass", 1000}, "gramos": {"mass", 1000}, "kg": {"mass", 1000000}, "kilogramos": {"mass", 1000000}, "t": {"mass", 1000000000}, "toneladas": {"mass", 1000000000},
	"ml": {"volume", 1}, "mililitros": {"volume", 1}, "l": {"volume", 1000}, "litros": {"volume", 1000},
	"segundos": {"duration", 1}, "minutos": {"duration", 60}, "horas": {"duration", 3600}, "dias": {"duration", 86400},
}
