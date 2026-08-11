      *****************************************************************
      * Appendix A.7's mixed record, unchanged.
      *
      * Fifteen bytes: 4 + 3 + 2 + 6. Three of the four axes bite on this record
      * at once -- charset on the two alphanumeric items, byte order on the
      * binary one, and neither on the packed one.
      *****************************************************************
       01  TXN.
           05  ID                      PIC X(4).
           05  AMT                     PIC S9(5) COMP-3.
           05  QTY                     PIC S9(4) COMP.
           05  NAME                    PIC X(6).
