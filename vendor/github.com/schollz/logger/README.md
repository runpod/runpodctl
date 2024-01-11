# logger

<img src="https://img.shields.io/badge/coverage-83%25-brightgreen.svg?style=flat-square" alt="Code coverage">&nbsp;<a href="https://travis-ci.org/schollz/logger"><img src="https://img.shields.io/travis/schollz/logger.svg?style=flat-square" alt="Build Status"></a>&nbsp;<a href="https://godoc.org/github.com/schollz/logger"><img src="http://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square" alt="Go Doc"></a> 

Simplistic, opinionated logging for Golang

- Zero dependencies
- Global logger (with optional local logger)
- Leveled
- Useful defaults / i.e. zero-config
- Simple API
- Colors on Linux (Windows colors are horrible and unnessecary)
- Set leveling via environmental variables `LOGGER=trace|debug|info|warn|error`

```
[trace] 20:04:57.670116 logger.go:125: trace shows granular timestamp and line info
[debug] 20:04:57 logger.go:129: debug shows regular timestamp and line info
[info]  2019/05/08 20:04:57 info shows timestamp
[warn]  2019/05/08 20:04:57 warn shows timestamp
[error] 2019/05/08 20:04:57 logger.go:141: error shows timestamp and line info
```

## Install

```
go get github.com/schollz/logger
```

## Usage 


```golang
package main

import (
	log "github.com/schollz/logger"
)

func main() {
	log.SetLevel("debug")
	log.Debug("hello, world")
}
```

## Contributing

Pull requests are welcome. Feel free to...

- Revise documentation
- Add new features
- Fix bugs
- Suggest improvements

## License

MIT
