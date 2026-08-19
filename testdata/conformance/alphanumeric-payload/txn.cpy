      *****************************************************************
      * The record of the alphanumeric-payload conformance entry.
      *
      * Two hundred and seventy bytes: a text key, a group of four items
      * the vendor's manual documents as carrying a payload rather than
      * characters, and a text name behind them.
      *
      * Every item here is PIC X. That is the point: a copybook has no
      * way to say which of them is text, so the layout beside it says
      * so and the four under TXN-PAYLOAD are the ones it names.
      *****************************************************************
       01  TXN-RECORD.
           05  TXN-KEY                 PIC X(4).
           05  TXN-PAYLOAD.
               10  TXN-STATUS          PIC X(1).
               10  TXN-REGION          PIC X(1).
               10  TXN-BYTES           PIC X(256).
               10  TXN-PAD             PIC X(2).
           05  TXN-NAME                PIC X(6).
