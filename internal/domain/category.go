package domain

// Category — категория траты. В БД и API хранится код, пользователю показывается название.
type Category string

const (
	CategoryFood          Category = "food"
	CategoryTransport     Category = "transport"
	CategoryHousing       Category = "housing"
	CategoryEntertainment Category = "entertainment"
	CategoryHealth        Category = "health"
	CategoryShopping      Category = "shopping"
	CategoryOther         Category = "other"
)

// Categories — все категории в порядке отображения.
var Categories = []Category{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryEntertainment,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

var categoryLabels = map[Category]string{
	CategoryFood:          "еда",
	CategoryTransport:     "транспорт",
	CategoryHousing:       "жильё",
	CategoryEntertainment: "развлечения",
	CategoryHealth:        "здоровье",
	CategoryShopping:      "покупки",
	CategoryOther:         "другое",
}

func (c Category) Valid() bool {
	_, ok := categoryLabels[c]
	return ok
}

// Label возвращает русское название категории.
func (c Category) Label() string {
	if l, ok := categoryLabels[c]; ok {
		return l
	}
	return string(c)
}
