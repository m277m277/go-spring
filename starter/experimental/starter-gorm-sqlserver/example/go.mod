module go-spring.org/starter-gorm-sqlserver/example

go 1.26.1

require gorm.io/gorm v1.31.1

require (
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	golang.org/x/text v0.40.0 // indirect
)

require go-spring.org/cloud v0.0.0

replace go-spring.org/cloud => ../../../../cloud
