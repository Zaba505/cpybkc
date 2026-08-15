      *****************************************************************
      * The record of the two delimited conformance entries.
      *
      * Seven bytes: a four-byte key and a packed amount of five digits,
      * two of them behind an implied decimal point.
      *
      * The amount is the field docs/ir/SPEC.md names where it says a
      * delimiter is not required to be absent from a record's data. A
      * PIC S9(3)V99 COMP-3 item holding +152.50 is the bytes 15 25 0C,
      * and 0x15 is the byte that ends a record in this file. A reader
      * that searches the input for the delimiter cuts that record after
      * four bytes; one that counts the extent never reads those bytes
      * as anything but the number they are.
      *****************************************************************
       01  LINE-RECORD.
           05  L-KEY                   PIC X(4).
           05  L-AMT                   PIC S9(3)V99 COMP-3.
