package bad

// Invalid: range over .Items without preceding assertion
var _ = Describe("Test", func() {
	It("checks items", func() {
		list := getList()
		for _, item := range list.Items { // want `range over \.Items without preceding non-empty assertion — add Expect\(x\.Items\)\.NotTo\(BeEmpty\(\)\) before the loop`
			Expect(item.Name).NotTo(BeEmpty())
		}
	})
})

// Invalid: range without assertion in nested Context
var _ = Describe("Test", func() {
	Context("nested", func() {
		It("checks items", func() {
			list := getList()
			for _, item := range list.Items { // want `range over \.Items without preceding non-empty assertion — add Expect\(x\.Items\)\.NotTo\(BeEmpty\(\)\) before the loop`
				Expect(item.Name).NotTo(BeEmpty())
			}
		})
	})
})

// Invalid: multiple range loops, second one missing assertion
var _ = Describe("Test", func() {
	It("checks multiple lists", func() {
		list1 := getList()
		Expect(list1.Items).NotTo(BeEmpty())
		for _, item := range list1.Items {
			Expect(item.Name).NotTo(BeEmpty())
		}

		list2 := getList()
		for _, item := range list2.Items { // want `range over \.Items without preceding non-empty assertion — add Expect\(x\.Items\)\.NotTo\(BeEmpty\(\)\) before the loop`
			Expect(item.Name).NotTo(BeEmpty())
		}
	})
})

// Invalid: assertion AFTER the range loop (not before)
var _ = Describe("Test", func() {
	It("checks items with late assertion", func() {
		list := getList()
		for _, item := range list.Items { // want `range over \.Items without preceding non-empty assertion — add Expect\(x\.Items\)\.NotTo\(BeEmpty\(\)\) before the loop`
			Expect(item.Name).NotTo(BeEmpty())
		}
		Expect(list.Items).NotTo(BeEmpty())
	})
})

// Invalid: When block with range loop missing assertion
var _ = Describe("Test", func() {
	When("condition is met", func() {
		It("checks items without assertion", func() {
			list := getList()
			for _, item := range list.Items { // want `range over \.Items without preceding non-empty assertion — add Expect\(x\.Items\)\.NotTo\(BeEmpty\(\)\) before the loop`
				Expect(item.Name).NotTo(BeEmpty())
			}
		})
	})
})

// Test helpers
type ItemList struct {
	Items []Item
}

type Item struct {
	Name string
}

func Describe(name string, f func()) bool { return true }
func Context(name string, f func())       {}
func When(name string, f func())          {}
func It(name string, f func())            {}
func Expect(val interface{}) Assertion    { return Assertion{} }

type Assertion struct{}

func (a Assertion) NotTo(matcher Matcher) {}

type Matcher struct{}

func BeEmpty() Matcher { return Matcher{} }

func getList() ItemList {
	return ItemList{Items: []Item{{Name: "test"}}}
}
