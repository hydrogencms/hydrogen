module github.com/hydrogencms/hydrogen/tests

go 1.26.0

require (
	github.com/hydrogencms/hydrogen v0.0.0
	github.com/hydrogencms/hydrogen/modules/postgres v0.0.0
	github.com/hydrogencms/hydrogen/modules/sqlite v0.0.0
)

replace (
	github.com/hydrogencms/hydrogen => ..
	github.com/hydrogencms/hydrogen/modules/postgres => ../modules/postgres
	github.com/hydrogencms/hydrogen/modules/sqlite => ../modules/sqlite
)
