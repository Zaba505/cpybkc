      *****************************************************************
      * A short and a long floating-point item.
      *
      * Twelve bytes. Neither item carries a PICTURE: COMP-1 and COMP-2 do not
      * permit one, and their widths come with the usage.
      *****************************************************************
       01  FLOAT-RECORD.
           05  F-SHORT                 COMP-1.
           05  F-LONG                  COMP-2.
