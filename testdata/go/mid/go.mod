module example.com/mid

go 1.26

require (
	example.com/leaf v0.1.0
	github.com/stretchr/testify v1.11.1
)

require example.com/other v0.1.0 // indirect
