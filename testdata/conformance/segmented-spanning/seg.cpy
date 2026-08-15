      *****************************************************************
      * The record of the segmented conformance entry.
      *
      * Twenty bytes: a six-byte key, eight bytes of text, a packed
      * count of five digits and a three-byte flag. No item here is
      * about a byte-level rule. What the record is for is being longer
      * than the largest segment its file admits, so that it arrives in
      * more than one piece however it was written.
      *
      * The widths are chosen so that every segment boundary in the file
      * falls inside an item rather than between two. There are three of
      * them: at twelve bytes in the first record and at eight in the
      * second, both in the middle of S-TEXT, and at sixteen in the
      * second, in the middle of S-COUNT. A consumer that decoded a
      * segment where it should have reassembled the record first fails
      * on those two items and on nothing else.
      *****************************************************************
       01  SEG-RECORD.
           05  S-KEY                   PIC X(6).
           05  S-TEXT                  PIC X(8).
           05  S-COUNT                 PIC S9(5) COMP-3.
           05  S-FLAG                  PIC X(3).
