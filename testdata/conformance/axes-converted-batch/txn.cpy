      *****************************************************************
      * The detail record of a batch: Appendix A.7's mixed record behind a
      * type code.
      *
      * Sixteen bytes: the type code, then A.7's four items unchanged --
      * an alphanumeric key, a packed amount, a binary quantity and a
      * name. The bytes of those four are the ones A.7 states.
      *****************************************************************
       01  TXN-RECORD.
           05  TXN-TYPE                PIC X(1).
           05  TXN-ID                  PIC X(4).
           05  TXN-AMT                 PIC S9(5) COMP-3.
           05  TXN-QTY                 PIC S9(4) COMP.
           05  TXN-NAME                PIC X(6).
