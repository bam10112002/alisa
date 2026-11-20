package ticktick

type Project struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color,omitempty"`
	Closed bool   `json:"closed"`
	Kind   string `json:"kind,omitempty"`
}

// Dates retain TickTick's yyyy-MM-dd'T'HH:mm:ssZ format and timezone offset.
type Task struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"projectId"`
	Title         string   `json:"title"`
	Content       string   `json:"content,omitempty"`
	Description   string   `json:"desc,omitempty"`
	Priority      int      `json:"priority"`
	Status        int      `json:"status"`
	StartDate     string   `json:"startDate,omitempty"`
	DueDate       string   `json:"dueDate,omitempty"`
	CompletedTime string   `json:"completedTime,omitempty"`
	TimeZone      string   `json:"timeZone,omitempty"`
	IsAllDay      bool     `json:"isAllDay"`
	Tags          []string `json:"tags,omitempty"`
	Items         []Item   `json:"items,omitempty"`
}

type Item struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status int    `json:"status"`
}
