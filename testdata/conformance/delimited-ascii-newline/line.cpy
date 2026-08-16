      *****************************************************************
      * The record of the ASCII line-sequential conformance entry.
      *
      * Six bytes: a four-byte key and a two-byte binary count.
      *
      * The count is the item the entry is about. A PIC S9(4) COMP item
      * holding 10 is the bytes 00 0A and one holding 2570 is 0A 0A, and
      * 0x0A is the byte that ends a record in this file. A reader that
      * searches the input for the delimiter cuts those records inside
      * the count -- the first after five bytes and the second after
      * four; one that counts the extent never reads those bytes as
      * anything but the number they are.
      *****************************************************************
       01  LINE-RECORD.
           05  L-KEY                   PIC X(4).
           05  L-QTY                   PIC S9(4) COMP.
