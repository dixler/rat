package sample

func inlineNestedControlFlow(values []int, input <-chan int, output chan<- int, err error) error {
	worker := func(limit int) error {
		if limit > 0 {
			for i := 0; i < limit; i++ {
				switch value := values[i%len(values)]; {
				case value < 0:
					return err
				case value == 0:
					select {
					case output <- value:
						continue
					case got := <-input:
						if got < 0 {
							return err
						}
					default:
						return nil
					}
				default:
					output <- value
				}
			}
		}
		return nil
	}
	return worker(len(values))
}

func inlineNestedDeclarationBlocks(flag bool, values []int, input <-chan int) int {
	return func() int {
		result := 0
		if flag {
			for index, value := range values {
				switch index % 2 {
				case 0:
					select {
					case got := <-input:
						result += got
					default:
						result += value
					}
				default:
					result -= value
				}
			}
		}
		return result
	}()
}
