// Package core 早返回版可用性检查. 用于 surnames 表的"该姓能生成名字吗"验证.
package core

// HasAnyPassingCandidate 检查给定姓氏在候选池中是否至少能产生 1 个通过所有规则的名字.
// 早返回: 找到第 1 个通过即 true; 全扫完无 true 则 false.
// 不排序不切片, 比 QueryNames 快 ~10-100x (大源 O(N) 内提前退出).
// 默认 avoid/must/style 均空, 与 /api/random 缺省参数等价.
func HasAnyPassingCandidate(candidateDb []CandidateName, charDb CharDb, surname string) (bool, error) {
	normalized := NormalizeQueryConfig(QueryConfig{Surname: surname})
	surnameLast, err := GetSurnameLastCharInfo(charDb, normalized.Surname)
	if err != nil {
		return false, err
	}
	for i := range candidateDb {
		candidate := &candidateDb[i]
		info0, ok0 := charDb[candidate.Chars[0]]
		info1, ok1 := charDb[candidate.Chars[1]]
		if !ok0 || !ok1 {
			continue
		}
		chars := [2]CharInfo{info0, info1}

		if !CheckMustRules(CheckMustInput{
			Candidate:    *candidate,
			Must:         normalized.Must,
			MustPosition: normalized.MustPosition,
		}) {
			continue
		}

		avoid := CheckAvoidRules(CheckAvoidInput{
			Candidate: *candidate,
			Surname:   normalized.Surname,
			Avoid:     normalized.Avoid,
			CharDb:    charDb,
		})
		if !avoid.Pass {
			continue
		}

		fullName := normalized.Surname + candidate.Name
		semantic := EvaluateSemanticSafety(fullName, candidate.Name)
		if !semantic.Pass {
			continue
		}

		phonetic := EvaluatePhonetics(surnameLast, chars[0], chars[1], normalized.Style)
		if !phonetic.Pass {
			continue
		}

		return true, nil
	}
	return false, nil
}