000010*  The header every record type of the file begins with.
000020     05  STD-HEADER.
000030         10  STD-REC-TYPE    PIC X.
000040             88  STD-IS-HEADER VALUE 'H'.
000050             88  STD-IS-DETAIL VALUE 'D'.
000060         10  STD-REC-LEN     PIC 9(4) COMP.
