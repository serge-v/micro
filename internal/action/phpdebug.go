package action

import (
	"log"

	"github.com/zyedidia/micro/v2/internal/xdebug"
)

var xc *xdebug.Client

func (h *BufPane) PhpCmd(args []string) {
	if xc == nil {
		p := struct {
			*BufPane
			*InfoPane
		}{
			h,
			InfoBar,
		}

		xc = &xdebug.Client{Editor: p}
	}

	if err := xc.ProcessCommand(args); err != nil {
		log.Println(err)
		xc.Editor.Error(err)
	}
}
